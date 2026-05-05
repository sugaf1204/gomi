package pxe

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
	"github.com/insomniacslk/dhcp/iana"

	"github.com/sugaf1204/gomi/internal/subnet"
)

const defaultLeaseTime = 1 * time.Hour

// Server holds DHCP server state for both full and proxy modes.
type Server struct {
	mu            sync.RWMutex
	mode          string // "full" or "proxy"
	iface         string
	serverIP      net.IP
	subnet        subnet.SubnetSpec
	boot          BootConfig
	localBootMACs map[string]struct{}
	leases        *leasePool // nil in proxy mode
	store         LeaseStore
}

// NewServer creates a DHCP server. mode must be "full" or "proxy".
func NewServer(mode, iface string, serverIP net.IP, spec subnet.SubnetSpec, boot BootConfig, store LeaseStore) *Server {
	s := &Server{
		mode:          mode,
		iface:         iface,
		serverIP:      serverIP.To4(),
		subnet:        spec,
		boot:          normalizeBootConfig(boot),
		localBootMACs: map[string]struct{}{},
		store:         store,
	}
	if mode == "full" && spec.PXEAddressRange != nil {
		s.leases = newLeasePool(spec.PXEAddressRange.Start, spec.PXEAddressRange.End, store)
	}
	return s
}

// Reconfigure updates the DHCP server's subnet configuration.
// If PXEAddressRange changed, the lease pool is rebuilt (existing leases are
// restored from the store). This is safe to call while the server is running.
func (s *Server) Reconfigure(spec subnet.SubnetSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldRange := s.subnet.PXEAddressRange
	s.subnet = spec

	if s.mode != "full" {
		return
	}

	// Determine if the lease pool needs to be rebuilt.
	needsRebuild := false
	switch {
	case oldRange == nil && spec.PXEAddressRange != nil:
		needsRebuild = true
	case oldRange != nil && spec.PXEAddressRange == nil:
		s.leases = nil
		log.Printf("dhcp: reconfigure: PXE range removed, lease pool disabled")
		return
	case oldRange != nil && spec.PXEAddressRange != nil:
		needsRebuild = oldRange.Start != spec.PXEAddressRange.Start || oldRange.End != spec.PXEAddressRange.End
	}

	if needsRebuild {
		s.leases = newLeasePool(spec.PXEAddressRange.Start, spec.PXEAddressRange.End, s.store)
		log.Printf("dhcp: reconfigure: lease pool rebuilt %s-%s", spec.PXEAddressRange.Start, spec.PXEAddressRange.End)
	}
}

// UpdateReservations updates static MAC→IP reservations on the lease pool.
func (s *Server) UpdateReservations(reservations map[string]net.IP) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leases != nil {
		s.leases.UpdateReservations(reservations)
	}
}

// UpdateLocalBootMACs updates the known MACs that should receive direct local
// boot assets instead of the installer iPXE binary on their first UEFI PXE hop.
func (s *Server) UpdateLocalBootMACs(macs map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]struct{}, len(macs))
	for raw := range macs {
		mac := strings.ToLower(strings.TrimSpace(raw))
		if mac != "" {
			next[mac] = struct{}{}
		}
	}
	s.localBootMACs = next
}

// ListenAndServe starts the DHCP server on UDP 67.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.leases != nil {
		log.Printf("dhcp: listening on %s mode=%s pool=%s-%s boot[bios=%s uefi=%s ipxe=%s]",
			s.iface, s.mode, s.subnet.PXEAddressRange.Start, s.subnet.PXEAddressRange.End,
			s.boot.BIOSBootFile, s.boot.UEFIBootFile, s.boot.IPXEScript)
	} else {
		log.Printf("dhcp: listening on %s mode=%s boot[bios=%s uefi=%s ipxe=%s]",
			s.iface, s.mode, s.boot.BIOSBootFile, s.boot.UEFIBootFile, s.boot.IPXEScript)
	}

	laddr := &net.UDPAddr{IP: net.IPv4zero, Port: 67}
	srv, err := server4.NewServer(s.iface, laddr, s.handler)
	if err != nil {
		return fmt.Errorf("dhcp: failed to create server: %w", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve() }()

	select {
	case <-ctx.Done():
		_ = srv.Close()
		return nil
	case err := <-errCh:
		return err
	}
}

// handler dispatches DISCOVER/REQUEST to full or proxy logic.
func (s *Server) handler(conn net.PacketConn, peer net.Addr, req *dhcpv4.DHCPv4) {
	if req.MessageType() != dhcpv4.MessageTypeDiscover && req.MessageType() != dhcpv4.MessageTypeRequest {
		return
	}

	s.mu.RLock()
	spec := s.subnet
	boot := s.boot
	localBootMACs := s.localBootMACs
	leases := s.leases
	s.mu.RUnlock()
	localBoot := isLocalBootMAC(req.ClientHWAddr, localBootMACs)

	var resp *dhcpv4.DHCPv4
	var err error

	switch s.mode {
	case "proxy":
		resp, err = s.handleProxy(req, boot, localBoot)
	default:
		resp, err = s.handleFull(req, spec, boot, localBoot, leases)
	}

	if err != nil {
		log.Printf("dhcp: handler error: %v", err)
		return
	}
	if resp == nil {
		return // intentionally skipped (e.g. non-PXE in proxy mode)
	}

	// RFC 2131 Section 4.1: response destination determination
	var dst net.Addr
	switch {
	case req.GatewayIPAddr != nil && !req.GatewayIPAddr.IsUnspecified():
		// Relay agent present → send back to relay
		dst = &net.UDPAddr{IP: req.GatewayIPAddr, Port: 67}
	case req.ClientIPAddr != nil && !req.ClientIPAddr.IsUnspecified():
		// Client already has an IP (RENEW/REBIND) → unicast
		dst = &net.UDPAddr{IP: req.ClientIPAddr, Port: 68}
	default:
		// DISCOVER/initial REQUEST → client has no IP yet → broadcast
		dst = &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	}

	if _, err := conn.WriteTo(resp.ToBytes(), dst); err != nil {
		log.Printf("dhcp: send error: %v", err)
	}
}

// handleFull processes DISCOVER/REQUEST with full IP allocation and boot info.
// spec and pool are snapshots taken under RLock by the caller.
func (s *Server) handleFull(req *dhcpv4.DHCPv4, spec subnet.SubnetSpec, boot BootConfig, localBoot bool, pool *leasePool) (*dhcpv4.DHCPv4, error) {
	var assignedIP net.IP
	if pool != nil {
		hostname := req.HostName()
		pxe := isPXEClient(req)
		assignedIP = pool.Allocate(req.ClientHWAddr, hostname, pxe)
		if assignedIP == nil {
			return nil, fmt.Errorf("lease pool exhausted for %s", req.ClientHWAddr)
		}
	} else {
		return nil, fmt.Errorf("full mode requires a lease pool")
	}

	respType := dhcpv4.MessageTypeOffer
	if req.MessageType() == dhcpv4.MessageTypeRequest {
		respType = dhcpv4.MessageTypeAck
	}

	// Lease time: use configured value or default
	leaseTime := defaultLeaseTime
	if spec.LeaseTime > 0 {
		leaseTime = time.Duration(spec.LeaseTime) * time.Second
	}

	modifiers := []dhcpv4.Modifier{
		dhcpv4.WithMessageType(respType),
		dhcpv4.WithServerIP(s.serverIP),
		dhcpv4.WithYourIP(assignedIP),
		dhcpv4.WithOption(dhcpv4.OptIPAddressLeaseTime(leaseTime)),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(s.serverIP)),
	}

	// Subnet mask from CIDR
	_, ipNet, err := net.ParseCIDR(spec.CIDR)
	if err == nil {
		modifiers = append(modifiers, dhcpv4.WithNetmask(ipNet.Mask))
	}

	// Router / default gateway
	if spec.DefaultGateway != "" {
		gw := net.ParseIP(spec.DefaultGateway)
		if gw != nil {
			modifiers = append(modifiers, dhcpv4.WithRouter(gw.To4()))
		}
	}

	// DNS servers
	if len(spec.DNSServers) > 0 {
		dns := make([]net.IP, 0, len(spec.DNSServers))
		for _, d := range spec.DNSServers {
			ip := net.ParseIP(d)
			if ip != nil {
				dns = append(dns, ip.To4())
			}
		}
		if len(dns) > 0 {
			modifiers = append(modifiers, dhcpv4.WithDNS(dns...))
		}
	}

	// Domain name (Option 15)
	if spec.DomainName != "" {
		modifiers = append(modifiers, dhcpv4.WithOption(dhcpv4.OptDomainName(spec.DomainName)))
	}

	// DNS search domains (Option 119)
	if len(spec.DNSSearchDomains) > 0 {
		modifiers = append(modifiers, dhcpv4.WithDomainSearchList(spec.DNSSearchDomains...))
	}

	// NTP servers (Option 42)
	if len(spec.NTPServers) > 0 {
		ntpIPs := make([]net.IP, 0, len(spec.NTPServers))
		for _, n := range spec.NTPServers {
			ip := net.ParseIP(n)
			if ip != nil {
				ntpIPs = append(ntpIPs, ip.To4())
			}
		}
		if len(ntpIPs) > 0 {
			modifiers = append(modifiers, dhcpv4.WithOption(dhcpv4.OptNTPServers(ntpIPs...)))
		}
	}

	// PXE boot info if the client is a PXE client
	if isPXEClient(req) {
		arch := clientArch(req)
		bootFile := selectBootFile(req, arch, boot, localBoot)
		modifiers = append(modifiers, withBootInfo(s.serverIP, bootFile))
	}

	resp, err := dhcpv4.NewReplyFromRequest(req, modifiers...)
	if err != nil {
		return nil, err
	}

	log.Printf("dhcp: %s %s -> %s mac=%s pxe=%v ipxe=%v localboot=%v arch=%v boot=%q",
		s.mode, respType, assignedIP, req.ClientHWAddr, isPXEClient(req), isIPXEClient(req), localBoot, clientArch(req), selectBootFile(req, clientArch(req), boot, localBoot))
	return resp, nil
}

// handleProxy processes DISCOVER/REQUEST returning only PXE boot information.
// Non-PXE clients are silently ignored.
func (s *Server) handleProxy(req *dhcpv4.DHCPv4, boot BootConfig, localBoot bool) (*dhcpv4.DHCPv4, error) {
	if !isPXEClient(req) {
		return nil, nil
	}

	respType := dhcpv4.MessageTypeOffer
	if req.MessageType() == dhcpv4.MessageTypeRequest {
		respType = dhcpv4.MessageTypeAck
	}

	modifiers := []dhcpv4.Modifier{
		dhcpv4.WithMessageType(respType),
		dhcpv4.WithServerIP(s.serverIP),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(s.serverIP)),
		withBootInfo(s.serverIP, selectBootFile(req, clientArch(req), boot, localBoot)),
	}

	resp, err := dhcpv4.NewReplyFromRequest(req, modifiers...)
	if err != nil {
		return nil, err
	}

	log.Printf("dhcp: proxy %s mac=%s arch=%v ipxe=%v localboot=%v boot=%q", respType, req.ClientHWAddr, clientArch(req), isIPXEClient(req), localBoot, selectBootFile(req, clientArch(req), boot, localBoot))
	return resp, nil
}

func normalizeBootConfig(c BootConfig) BootConfig {
	if strings.TrimSpace(c.BIOSBootFile) == "" {
		c.BIOSBootFile = "undionly.kpxe"
	}
	if strings.TrimSpace(c.UEFIBootFile) == "" {
		c.UEFIBootFile = "ipxe.efi"
	}
	if strings.TrimSpace(c.UEFILocalBootFile) == "" {
		c.UEFILocalBootFile = "ipxe.efi"
	}
	return c
}

func vendorClass(req *dhcpv4.DHCPv4) string {
	return string(req.Options.Get(dhcpv4.OptionClassIdentifier))
}

// isPXEClient checks Option 60 (Vendor Class Identifier) for PXE/iPXE.
func isPXEClient(req *dhcpv4.DHCPv4) bool {
	vc := vendorClass(req)
	return strings.HasPrefix(vc, "PXEClient") || strings.HasPrefix(vc, "iPXE")
}

// isIPXEClient checks Option 60 (Vendor Class Identifier) for iPXE.
func isIPXEClient(req *dhcpv4.DHCPv4) bool {
	if strings.HasPrefix(strings.ToLower(vendorClass(req)), "ipxe") {
		return true
	}
	for _, uc := range req.UserClass() {
		if strings.Contains(strings.ToLower(strings.TrimSpace(uc)), "ipxe") {
			return true
		}
	}
	return false
}

// clientArch returns the client system architecture from Option 93.
func clientArch(req *dhcpv4.DHCPv4) iana.Arch {
	archs := req.ClientArch()
	if len(archs) > 0 {
		return archs[0]
	}
	return iana.INTEL_X86PC // default to BIOS
}

func selectBootFile(req *dhcpv4.DHCPv4, arch iana.Arch, boot BootConfig, localBoot bool) string {
	if isIPXEClient(req) && strings.TrimSpace(boot.IPXEScript) != "" {
		script := boot.IPXEScript
		mac := strings.ToLower(strings.TrimSpace(req.ClientHWAddr.String()))
		if mac == "" {
			return script
		}
		sep := "?"
		if strings.Contains(script, "?") {
			sep = "&"
		}
		return script + sep + "mac=" + url.QueryEscape(mac)
	}
	switch arch {
	case iana.EFI_BC, iana.EFI_X86_64:
		if localBoot && strings.TrimSpace(boot.UEFILocalBootFile) != "" {
			return boot.UEFILocalBootFile
		}
		return boot.UEFIBootFile
	default:
		return boot.BIOSBootFile
	}
}

func isLocalBootMAC(mac net.HardwareAddr, localBootMACs map[string]struct{}) bool {
	if len(localBootMACs) == 0 {
		return false
	}
	_, ok := localBootMACs[strings.ToLower(strings.TrimSpace(mac.String()))]
	return ok
}

// withBootInfo sets TFTP server name (siaddr / Option 66) and boot filename (Option 67).
func withBootInfo(serverIP net.IP, bootFile string) dhcpv4.Modifier {
	return func(resp *dhcpv4.DHCPv4) {
		resp.ServerIPAddr = serverIP
		resp.UpdateOption(dhcpv4.OptTFTPServerName(serverIP.String()))
		resp.BootFileName = bootFile
		resp.UpdateOption(dhcpv4.OptBootFileName(bootFile))
	}
}
