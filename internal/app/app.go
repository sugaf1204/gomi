package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/bootenv"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/discovery"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	"github.com/sugaf1204/gomi/internal/infra/config"
	"github.com/sugaf1204/gomi/internal/infra/dns"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/provision"
	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

type Runtime struct {
	Config          config.Config
	machineStore    machine.Store
	subnetStore     subnet.Store
	authStore       auth.Store
	sshkeyStore     sshkey.Store
	hwinfoStore     hwinfo.Store
	hypervisorStore hypervisor.Store
	hvTokenStore    hypervisor.TokenStore
	agentTokenStore hypervisor.AgentTokenStore
	vmStore         vm.Store
	cloudInitStore  cloudinit.Store
	osimageStore    osimage.Store
	leaseStore      pxe.LeaseStore
	machineSvc      *machine.Service
	sshkeySvc       *sshkey.Service
	hwinfoSvc       *hwinfo.Service
	discoverySvc    *discovery.Service
	hypervisorSvc   *hypervisor.Service
	vmSvc           *vm.Service
	cloudInitSvc    *cloudinit.Service
	osimageSvc      *osimage.Service
	dnsClient       *dns.PowerDNSClient
	dnsController   dns.Controller
	executor        *power.Executor
	healthCheck     func(ctx context.Context) error
	frontendFS      fs.FS
	pxeMu           sync.Mutex
	dhcpServer      *pxe.Server
	tftpServer      *pxe.TFTPServer
	pxeCancel       context.CancelFunc
	pxeDone         chan struct{}
	pxeState        pxeRuntimeState
	bootenvMgr      *bootenv.Manager
	closers         []func()
}

type pxeRuntimeState struct {
	mode     string
	iface    string
	serverIP string
	tftpAddr string
	tftpRoot string
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	cfg.DNSMode = config.NormalizeDNSMode(cfg.DNSMode)
	switch cfg.DNSMode {
	case config.DNSModeOff, config.DNSModeEmbedded, config.DNSModePowerDNS, config.DNSModeRFC2136:
	default:
		return nil, fmt.Errorf("unsupported dns mode: %s", cfg.DNSMode)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}

	rt := &Runtime{
		Config:    cfg,
		dnsClient: dns.NewPowerDNSClient(cfg.PowerDNSBaseURL, cfg.PowerDNSAPIToken, cfg.PowerDNSServerID),
		executor:  power.NewExecutor(),
	}

	if err := rt.buildStores(cfg); err != nil {
		rt.Close()
		return nil, err
	}

	rt.machineSvc = machine.NewService(rt.machineStore, machine.WithProvisionTimeout(cfg.ProvisionTimeout))
	rt.discoverySvc = discovery.NewService(rt.machineStore)
	rt.sshkeySvc = sshkey.NewService(rt.sshkeyStore)
	rt.hwinfoSvc = hwinfo.NewService(rt.hwinfoStore)
	rt.hypervisorSvc = hypervisor.NewService(rt.hypervisorStore, rt.hvTokenStore, rt.agentTokenStore)
	rt.vmSvc = vm.NewService(rt.vmStore)
	rt.cloudInitSvc = cloudinit.NewService(rt.cloudInitStore)
	rt.osimageSvc = osimage.NewService(rt.osimageStore)
	rt.bootenvMgr = bootenv.NewManager(bootenv.Config{
		DataDir:   cfg.DataDir,
		FilesDir:  filepath.Join(cfg.DataDir, "files"),
		SourceURL: cfg.BootenvSourceURL,
	})

	if err := rt.bootstrap(context.Background()); err != nil {
		rt.Close()
		return nil, err
	}

	return rt, nil
}

func (r *Runtime) buildStores(cfg config.Config) error {
	backend, err := infrasql.New(cfg.DBDriver, cfg.DBDsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	r.closers = append(r.closers, func() { _ = backend.Close() })

	if err := backend.Migrate(); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	log.Printf("database ready: driver=%s", cfg.DBDriver)

	r.machineStore = backend.Machines()
	r.subnetStore = backend.Subnets()
	r.authStore = backend.Auth()
	r.sshkeyStore = backend.SSHKeys()
	r.hwinfoStore = backend.HWInfo()
	r.hypervisorStore = backend.Hypervisors()
	r.hvTokenStore = backend.HypervisorTokens()
	r.agentTokenStore = backend.AgentTokens()
	r.vmStore = backend.VMs()
	r.cloudInitStore = backend.CloudInits()
	r.osimageStore = backend.OSImages()
	r.leaseStore = backend.DHCPLeases()
	r.healthCheck = backend.Health
	return nil
}

func (r *Runtime) StartServer(ctx context.Context) error {
	pxeBaseURL := strings.TrimRight(r.Config.PXEHTTPBaseURL, "/")

	vmDeployer := &vm.Deployer{
		Hypervisors: r.hypervisorSvc,
		OSImages:    r.osimageSvc,
		VMs:         r.vmSvc,
		PXEBaseURL:  pxeBaseURL,
	}
	vmRuntimeSyncer := &vm.RuntimeSyncer{
		Hypervisors: r.hypervisorSvc,
		VMs:         r.vmSvc,
	}
	vmMigrator := &vm.Migrator{
		Hypervisors: r.hypervisorSvc,
		VMs:         r.vmSvc,
	}

	r.configureDNSController()

	srv := infraapi.NewServer(infraapi.ServerConfig{
		Machines:         r.machineSvc,
		PowerExecutor:    r.executor,
		Subnets:          r.subnetStore,
		AuthStore:        r.authStore,
		AuthService:      infraapi.NewAuthService(r.authStore, r.Config.SessionTTL),
		Discovery:        r.discoverySvc,
		SSHKeys:          r.sshkeySvc,
		HWInfo:           r.hwinfoSvc,
		Hypervisors:      r.hypervisorSvc,
		AgentTokenStore:  r.agentTokenStore,
		VMs:              r.vmSvc,
		CloudInits:       r.cloudInitSvc,
		OSImages:         r.osimageSvc,
		LeaseStore:       r.leaseStore,
		FilesDir:         filepath.Join(r.Config.DataDir, "files"),
		ImageStorageDir:  filepath.Join(r.Config.DataDir, "images"),
		HealthCheck:      r.healthCheck,
		FrontendFS:       r.frontendFS,
		PXEHTTPBaseURL:   pxeBaseURL,
		PXETFTPRoot:      r.Config.TFTPRoot,
		ProvisionTimeout: r.Config.ProvisionTimeout,
		VMDeployer:       vmDeployer,
		VMRuntimeSyncer:  vmRuntimeSyncer,
		VMMigrator:       vmMigrator,
		BootEnvs:         r.bootenvMgr,
	})

	if r.Config.BackgroundSyncEnabled {
		go r.runSyncLoop(ctx)
		go r.runPowerPollLoop(ctx)
	}

	go r.runPXEManager(ctx)
	if r.Config.DNSMode != config.DNSModeOff {
		go r.startDNS(ctx)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(r.Config.ListenAddr); err != nil && !errors.Is(err, gohttp.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (r *Runtime) runSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.syncProvisioningStates(ctx)
		}
	}
}

func (r *Runtime) syncProvisioningStates(ctx context.Context) {
	machines, err := r.machineStore.List(ctx)
	if err != nil {
		log.Printf("sync: list error: %v", err)
		return
	}

	for _, m := range machines {
		// Provisioning timeout check
		if m.Provision != nil && m.Provision.Active && m.Provision.DeadlineAt != nil {
			if time.Now().UTC().After(*m.Provision.DeadlineAt) {
				m.Provision.Active = false
				m.Phase = machine.PhaseError
				m.LastError = "provisioning timed out"
				m.UpdatedAt = time.Now().UTC()
				if err := r.machineStore.Upsert(ctx, m); err != nil {
					log.Printf("sync: timeout save error: %v", err)
				}
				continue
			}
		}

		if m.Phase != machine.PhaseProvisioning {
			continue
		}
		if m.Provision != nil && m.Provision.FinishedAt != nil {
			continue
		}
		sshKeys, _ := r.sshkeyStore.List(ctx)
		artifacts, cfg, buildErr := provision.BuildArtifacts(m, r.Config.BootHTTPBaseURL, sshKeys)
		result := machine.SyncState(m, artifacts, cfg, buildErr)
		if result.NeedsSave {
			if err := r.machineStore.Upsert(ctx, result.Machine); err != nil {
				log.Printf("sync: save error: %v", err)
			}
		}
		if result.NeedsDNS {
			r.syncDNS(ctx)
		}
	}
}

func (r *Runtime) startDNS(ctx context.Context) {
	if r.dnsController == nil {
		return
	}

	if notifier, ok := r.machineStore.(machine.ChangeNotifier); ok {
		notifier.Subscribe(func() {
			r.syncDNS(ctx)
		})
	}
	if r.Config.DNSMode == config.DNSModeEmbedded || r.Config.DNSMode == config.DNSModeRFC2136 {
		r.subscribeEmbeddedDNSChanges(ctx)
	}
	go r.runDNSSyncLoop(ctx)

	if err := r.dnsController.Start(ctx); err != nil {
		log.Printf("dns: controller error: %v", err)
	}
}

func (r *Runtime) configureDNSController() {
	switch r.Config.DNSMode {
	case config.DNSModeEmbedded:
		r.dnsController = dns.NewEmbeddedServer(dns.EmbeddedConfig{
			Addr:               r.Config.DNSEmbeddedAddr,
			TTL:                r.Config.DNSTTL,
			DynamicRecordsPath: r.Config.DataDir + "/dns-records.json",
			Machines:           r.machineStore,
			VMs:                r.vmStore,
			Subnets:            r.subnetStore,
		})
	case config.DNSModePowerDNS:
		r.dnsController = dns.NewPowerDNSController(r.dnsClient, r.machineStore)
	case config.DNSModeRFC2136:
		r.dnsController = dns.NewRFC2136Controller(dns.RFC2136Config{
			Server:        r.Config.RFC2136Server,
			Zone:          r.Config.RFC2136Zone,
			TTL:           r.Config.DNSTTL,
			TSIGName:      r.Config.RFC2136TSIGName,
			TSIGSecret:    r.Config.RFC2136TSIGSecret,
			TSIGAlgorithm: r.Config.RFC2136TSIGAlgorithm,
			Transport:     r.Config.RFC2136Transport,
			Machines:      r.machineStore,
			VMs:           r.vmStore,
			Subnets:       r.subnetStore,
		})
	default:
		r.dnsController = nil
	}
}

func (r *Runtime) subscribeEmbeddedDNSChanges(ctx context.Context) {
	if notifier, ok := r.subnetStore.(subnet.ChangeNotifier); ok {
		notifier.Subscribe(func() {
			r.syncDNS(ctx)
		})
	}
	if notifier, ok := r.vmStore.(vm.ChangeNotifier); ok {
		notifier.Subscribe(func() {
			r.syncDNS(ctx)
		})
	}
}

func (r *Runtime) runDNSSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.syncDNS(ctx)
		}
	}
}

func (r *Runtime) syncDNS(ctx context.Context) {
	if r.dnsController == nil {
		return
	}
	if err := r.dnsController.Sync(ctx); err != nil {
		log.Printf("dns: sync error: %v", err)
	}
}

// runPowerPollLoop periodically checks power state for all machines and
// synchronizes DHCP lease IPs.
func (r *Runtime) runPowerPollLoop(ctx context.Context) {
	leaseTicker := time.NewTicker(30 * time.Second)
	powerTicker := time.NewTicker(2 * time.Second)
	defer leaseTicker.Stop()
	defer powerTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-leaseTicker.C:
			r.syncLeaseIPs(ctx)
		case <-powerTicker.C:
			r.pollPowerStates(ctx)
		}
	}
}

// syncLeaseIPs synchronizes DHCP lease IPs to matching machines.
func (r *Runtime) syncLeaseIPs(ctx context.Context) {
	ipUpdater, ok := r.machineStore.(machine.IPAddressUpdater)
	if !ok {
		log.Printf("lease-sync: machine store does not support partial IP updates")
		return
	}
	machines, err := r.machineStore.List(ctx)
	if err != nil {
		return
	}
	leases, err := r.leaseStore.List(ctx)
	if err != nil {
		return
	}

	leaseByMAC := make(map[string]pxe.DHCPLease, len(leases))
	for _, l := range leases {
		leaseByMAC[strings.ToLower(l.MAC)] = l
	}

	for _, m := range machines {
		// Static IP machines manage their own IP; don't overwrite from DHCP leases.
		if m.IPAssignment == machine.IPAssignmentModeStatic {
			continue
		}
		mac := strings.ToLower(m.MAC)
		lease, ok := leaseByMAC[mac]
		if !ok || lease.IP == "" {
			continue
		}
		if m.IP == lease.IP {
			continue
		}
		if err := ipUpdater.UpdateDynamicIPAddress(ctx, m.Name, m.MAC, lease.IP, time.Now().UTC()); err != nil {
			log.Printf("lease-sync: failed to update IP for %s: %v", m.Name, err)
		}
	}
}

// pollPowerStates checks the power state of each machine and updates status.
func (r *Runtime) pollPowerStates(ctx context.Context) {
	stateUpdater, ok := r.machineStore.(machine.PowerStateStatusUpdater)
	if !ok {
		log.Printf("power-state: machine store does not support partial power-state updates")
		return
	}
	machines, err := r.machineStore.List(ctx)
	if err != nil {
		return
	}

	for _, m := range machines {
		mi := power.MachineInfo{
			Name:     m.Name,
			Hostname: m.Hostname,
			MAC:      m.MAC,
			IP:       m.IP,
			Power:    m.Power,
		}
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		state, err := r.executor.CheckStatus(checkCtx, mi)
		cancel()
		if err != nil {
			continue
		}
		if state == m.PowerState {
			continue
		}
		now := time.Now().UTC()
		if err := stateUpdater.UpdatePowerStateStatus(ctx, m.Name, state, now, now); err != nil {
			log.Printf("power-state: failed to update %s: %v", m.Name, err)
		}
	}
}

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
		UEFILocalBootFile: "grubnetx64.efi",
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

const uefiLocalBootGRUBConfig = "exit 1\n"

var uefiLocalBootGRUBCandidates = []string{
	"/usr/lib/grub/x86_64-efi-signed/grubnetx64.efi.signed",
	"/usr/lib/grub/x86_64-efi/monolithic/grubnetx64.efi",
}

type tftpBootAsset struct {
	dst        string
	candidates []string
	hint       string
}

var ipxeBootAssets = []tftpBootAsset{
	{
		dst:        "ipxe.efi",
		candidates: []string{"/usr/lib/ipxe/ipxe.efi"},
		hint:       "install ipxe",
	},
	{
		dst:        "undionly.kpxe",
		candidates: []string{"/usr/lib/ipxe/undionly.kpxe"},
		hint:       "install ipxe",
	},
}

func ensureTFTPBootAssets(tftpRoot string) error {
	if err := ensureUEFILocalBootGRUBAssets(tftpRoot); err != nil {
		return err
	}
	return ensureIPXEBootAssets(tftpRoot)
}

func ensureUEFILocalBootGRUBAssets(tftpRoot string) error {
	root := strings.TrimSpace(tftpRoot)
	if root == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(root, "grub"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "grub", "grub.cfg"), []byte(uefiLocalBootGRUBConfig), 0o644); err != nil {
		return err
	}
	dst := filepath.Join(root, "grubnetx64.efi")
	for _, src := range uefiLocalBootGRUBCandidates {
		if err := copyFileIfChanged(src, dst, 0o644); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		log.Printf("tftp: installed UEFI local boot GRUB asset %s from %s", dst, src)
		return nil
	}

	return fmt.Errorf("grubnetx64.efi not found; install grub-efi-amd64-signed or grub-efi-amd64-bin")
}

func ensureIPXEBootAssets(tftpRoot string) error {
	root := strings.TrimSpace(tftpRoot)
	if root == "" {
		return nil
	}
	for _, asset := range ipxeBootAssets {
		if err := installTFTPBootAsset(root, asset); err != nil {
			return err
		}
	}
	return nil
}

func installTFTPBootAsset(tftpRoot string, asset tftpBootAsset) error {
	dst := filepath.Join(tftpRoot, asset.dst)
	for _, src := range asset.candidates {
		if err := copyFileIfChanged(src, dst, 0o644); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		log.Printf("tftp: installed PXE boot asset %s from %s", dst, src)
		return nil
	}
	return fmt.Errorf("%s not found; %s", asset.dst, asset.hint)
}

func copyFileIfChanged(src, dst string, mode fs.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if current, err := os.ReadFile(dst); err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return err
	}
	return nil
}

func (r *Runtime) currentPXEState() (*pxe.Server, pxeRuntimeState) {
	r.pxeMu.Lock()
	defer r.pxeMu.Unlock()
	return r.dhcpServer, r.pxeState
}

func (r *Runtime) stopPXE(reason string) {
	r.pxeMu.Lock()
	cancel := r.pxeCancel
	done := r.pxeDone
	r.dhcpServer = nil
	r.tftpServer = nil
	r.pxeCancel = nil
	r.pxeDone = nil
	r.pxeState = pxeRuntimeState{}
	r.pxeMu.Unlock()

	if cancel != nil {
		log.Printf("dhcp: stopping PXE services: %s", reason)
		cancel()
		if done != nil {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				log.Printf("dhcp: timed out waiting for PXE services to stop")
			}
		}
	}
}

func (r *Runtime) clearPXEIfCurrent(server *pxe.Server, cancel context.CancelFunc) {
	r.pxeMu.Lock()
	if r.dhcpServer != server {
		r.pxeMu.Unlock()
		return
	}
	r.dhcpServer = nil
	r.tftpServer = nil
	r.pxeCancel = nil
	r.pxeDone = nil
	r.pxeState = pxeRuntimeState{}
	r.pxeMu.Unlock()
	cancel()
}

// addStaticReservation is a shared helper for building DHCP reservation maps.
func addStaticReservation(reservations map[string]net.IP, h node.Node) {
	if h.GetIPAssignment() != resource.IPAssignmentStatic {
		return
	}
	mac := h.PrimaryMAC()
	ip := net.ParseIP(strings.TrimSpace(h.StaticIP()))
	if mac != "" && ip != nil {
		reservations[mac] = ip.To4()
	}
}

func addLocalBootMAC(localBootMACs map[string]struct{}, h node.Node) {
	if h == nil || !shouldDirectLocalBoot(h) {
		return
	}
	for _, raw := range h.AllMACs() {
		mac := strings.ToLower(strings.TrimSpace(raw))
		if mac != "" {
			localBootMACs[mac] = struct{}{}
		}
	}
}

func shouldDirectLocalBoot(h node.Node) bool {
	if !h.IsProvisioningActive() {
		return true
	}
	m, ok := h.(*machine.Machine)
	if !ok || m.Provision == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m.Provision.Artifacts["imageApplied"]), "true")
}

// syncDHCPReservations builds a MAC→IP reservation map from subnet manual
// reservations and static-IP machines/VMs, then pushes it to the DHCP server.
func (r *Runtime) syncDHCPReservations(ctx context.Context) {
	dhcpSrv, _ := r.currentPXEState()
	if dhcpSrv == nil {
		return
	}

	reservations := make(map[string]net.IP)
	localBootMACs := make(map[string]struct{})

	// 1. Subnet manual reservations
	subnets, err := r.subnetStore.List(ctx)
	if err == nil && len(subnets) > 0 {
		for _, res := range subnets[0].Spec.Reservations {
			mac := strings.ToLower(strings.TrimSpace(res.MAC))
			ip := net.ParseIP(strings.TrimSpace(res.IP))
			if mac != "" && ip != nil {
				reservations[mac] = ip.To4()
			}
		}
	}

	// 2. Static machines
	machines, err := r.machineStore.List(ctx)
	if err == nil {
		for i := range machines {
			addStaticReservation(reservations, &machines[i])
			addLocalBootMAC(localBootMACs, &machines[i])
		}
	}

	// 3. Static VMs
	vms, vmErr := r.vmStore.List(ctx)
	if vmErr == nil {
		for i := range vms {
			addStaticReservation(reservations, &vms[i])
			addLocalBootMAC(localBootMACs, &vms[i])
		}
	}

	dhcpSrv.UpdateReservations(reservations)
	dhcpSrv.UpdateLocalBootMACs(localBootMACs)
	log.Printf("dhcp: reservation sync: %d reservations, %d direct local boot macs", len(reservations), len(localBootMACs))
}

// detectDefaultIface returns the name of the first non-loopback, up interface.
func detectDefaultIface() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagLoopback != 0 || i.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				return i.Name
			}
		}
	}
	return ""
}

// detectIfaceIP returns the first IPv4 address on the given interface.
func detectIfaceIP(name string) net.IP {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.To4()
		}
	}
	return nil
}

func (r *Runtime) resolvePXEHTTPBaseURL(serverIP net.IP) string {
	if strings.TrimSpace(r.Config.PXEHTTPBaseURL) != "" {
		return strings.TrimRight(r.Config.PXEHTTPBaseURL, "/")
	}
	port := listenPort(r.Config.ListenAddr)
	return fmt.Sprintf("http://%s:%s/pxe", serverIP.String(), port)
}

func listenPort(addr string) string {
	if strings.HasPrefix(addr, ":") {
		p := strings.TrimPrefix(addr, ":")
		if p != "" {
			return p
		}
	}
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if host != "" || port != "" {
			return port
		}
	}
	return "8080"
}

func (r *Runtime) SetFrontendFS(fsys fs.FS) {
	r.frontendFS = fsys
}

func (r *Runtime) Close() {
	for _, fn := range r.closers {
		fn()
	}
}
