package app

import (
	"context"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

func (r *Runtime) runPXEManager(ctx context.Context) {
	trigger := make(chan struct{}, 1)
	notify := func() {
		select {
		case trigger <- struct{}{}:
		default:
		}
	}

	if notifier, ok := r.subnetStore.(subnet.ChangeNotifier); ok {
		notifier.Subscribe(notify)
	}
	if notifier, ok := r.machineStore.(machine.ChangeNotifier); ok {
		notifier.Subscribe(notify)
	}
	if notifier, ok := r.vmStore.(vm.ChangeNotifier); ok {
		notifier.Subscribe(notify)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	r.reconcilePXE(ctx)

	for {
		select {
		case <-ctx.Done():
			r.stopPXE("runtime stopped")
			return
		case <-trigger:
			r.reconcilePXE(ctx)
		case <-ticker.C:
			r.reconcilePXE(ctx)
		}
	}
}

func (r *Runtime) reconcilePXE(ctx context.Context) {
	mode := strings.ToLower(strings.TrimSpace(r.Config.DHCPMode))
	if mode == "" {
		mode = "full"
	}
	if mode == "off" {
		r.stopPXE("DHCP disabled")
		return
	}
	if mode != "full" && mode != "proxy" {
		log.Printf("dhcp: unsupported mode %q, stopping PXE services", r.Config.DHCPMode)
		r.stopPXE("unsupported DHCP mode")
		return
	}

	subnets, err := r.subnetStore.List(ctx)
	if err != nil {
		log.Printf("dhcp: list subnets failed: %v", err)
		r.stopPXE("subnet list failed")
		return
	}
	if len(subnets) == 0 {
		r.stopPXE("no subnets configured")
		return
	}

	sub := subnets[0]
	spec := sub.Spec
	if !pxeSubnetReady(mode, spec) {
		r.stopPXE("PXE address range not configured")
		return
	}

	iface := strings.TrimSpace(r.Config.DHCPIface)
	if iface == "" {
		if spec.PXEInterface != "" {
			iface = spec.PXEInterface
		} else {
			iface = detectDefaultIface()
		}
	}
	if iface == "" {
		log.Printf("dhcp: unable to determine network interface, stopping PXE services")
		r.stopPXE("DHCP interface unavailable")
		return
	}

	serverIP := detectIfaceIP(iface)
	if serverIP == nil {
		log.Printf("dhcp: unable to determine server IP for %s, stopping PXE services", iface)
		r.stopPXE("DHCP server IP unavailable")
		return
	}

	state := pxeRuntimeState{
		mode:     mode,
		iface:    iface,
		serverIP: serverIP.String(),
		tftpAddr: r.Config.TFTPAddr,
		tftpRoot: r.Config.TFTPRoot,
	}

	pxeHTTPBaseURL := r.resolvePXEHTTPBaseURL(serverIP)
	boot := pxe.BootConfig{
		BIOSBootFile:      r.Config.PXEBootFileBIOS,
		UEFIBootFile:      r.Config.PXEBootFileUEFI,
		UEFILocalBootFile: "ipxe.efi",
		IPXEScript:        strings.TrimRight(pxeHTTPBaseURL, "/") + "/boot.ipxe",
	}

	if srv, current := r.currentPXEState(); srv != nil && current == state {
		log.Printf("dhcp: sync: subnet %q reconfiguring", sub.Name)
		srv.Reconfigure(spec)
		r.syncDHCPReservations(ctx)
		return
	}

	r.stopPXE("PXE configuration changed")
	r.startPXE(ctx, state, spec, boot, pxeHTTPBaseURL)
}

func pxeSubnetReady(mode string, spec subnet.SubnetSpec) bool {
	switch mode {
	case "proxy":
		return true
	case "full":
		return spec.PXEAddressRange != nil
	default:
		return false
	}
}

func (r *Runtime) startPXE(parent context.Context, state pxeRuntimeState, spec subnet.SubnetSpec, boot pxe.BootConfig, pxeHTTPBaseURL string) {
	if err := os.MkdirAll(r.Config.TFTPRoot, 0o755); err != nil {
		log.Printf("tftp: failed to create tftp root %q: %v", r.Config.TFTPRoot, err)
		return
	}
	if err := ensureTFTPBootAssets(r.Config.TFTPRoot); err != nil {
		log.Printf("tftp: boot asset setup failed: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	var wg sync.WaitGroup
	dhcpSrv := pxe.NewServer(state.mode, state.iface, net.ParseIP(state.serverIP), spec, boot, r.leaseStore)
	tftpSrv := pxe.NewTFTPServer(r.Config.TFTPAddr, r.Config.TFTPRoot)

	r.pxeMu.Lock()
	if r.pxeCancel != nil {
		r.pxeCancel()
	}
	r.dhcpServer = dhcpSrv
	r.tftpServer = tftpSrv
	r.pxeCancel = cancel
	r.pxeDone = done
	r.pxeState = state
	r.pxeMu.Unlock()

	r.syncDHCPReservations(parent)

	wg.Add(2)
	go func() {
		defer wg.Done()
		log.Printf("tftp: listening on %s root=%s", r.Config.TFTPAddr, r.Config.TFTPRoot)
		if err := tftpSrv.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
			log.Printf("tftp: server error: %v", err)
			r.clearPXEIfCurrent(dhcpSrv, cancel)
		}
	}()

	go func() {
		defer wg.Done()
		log.Printf("dhcp: resolved iface=%s server_ip=%s pxe_http_base=%s", state.iface, state.serverIP, pxeHTTPBaseURL)
		if err := dhcpSrv.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
			log.Printf("dhcp: server error: %v", err)
			r.clearPXEIfCurrent(dhcpSrv, cancel)
		}
	}()

	go func() {
		wg.Wait()
		close(done)
	}()
}
