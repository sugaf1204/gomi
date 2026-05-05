package dns

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	dnsv2 "codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

var _ Controller = (*EmbeddedServer)(nil)
var _ dnsv2.Handler = (*EmbeddedServer)(nil)

type EmbeddedConfig struct {
	Addr               string
	TTL                time.Duration
	DynamicRecordsPath string
	Machines           machine.Store
	VMs                vm.Store
	Subnets            subnet.Store
}

type EmbeddedServer struct {
	addr               string
	ttl                uint32
	dynamicRecordsPath string
	machines           machine.Store
	vms                vm.Store
	subnets            subnet.Store

	mu             sync.RWMutex
	zones          map[string]struct{}
	records        map[string][]netip.Addr
	dynamicRecords map[string]map[uint16]dynamicRecordSet
	dynamicLoaded  bool
	serialAt       time.Time
}

func NewEmbeddedServer(cfg EmbeddedConfig) *EmbeddedServer {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		addr = ":53"
	}
	ttl := uint32(300)
	if cfg.TTL > 0 {
		ttl = uint32(cfg.TTL.Seconds())
	}
	return &EmbeddedServer{
		addr:               addr,
		ttl:                ttl,
		dynamicRecordsPath: strings.TrimSpace(cfg.DynamicRecordsPath),
		machines:           cfg.Machines,
		vms:                cfg.VMs,
		subnets:            cfg.Subnets,
		zones:              map[string]struct{}{},
		records:            map[string][]netip.Addr{},
		dynamicRecords:     map[string]map[uint16]dynamicRecordSet{},
	}
}

func (s *EmbeddedServer) Start(ctx context.Context) error {
	if err := s.Sync(ctx); err != nil {
		return err
	}

	udpConn, err := net.ListenPacket("udp", s.addr)
	if err != nil {
		return fmt.Errorf("dns embedded udp listen %s: %w", s.addr, err)
	}
	tcpLn, err := net.Listen("tcp", s.addr)
	if err != nil {
		_ = udpConn.Close()
		return fmt.Errorf("dns embedded tcp listen %s: %w", s.addr, err)
	}

	udpServer := dnsv2.NewServer()
	udpServer.Net = "udp"
	udpServer.PacketConn = udpConn
	udpServer.Handler = s

	tcpServer := dnsv2.NewServer()
	tcpServer.Net = "tcp"
	tcpServer.Listener = tcpLn
	tcpServer.Handler = s

	log.Printf("dns: embedded authoritative server listening udp=%s tcp=%s ttl=%ds", udpConn.LocalAddr(), tcpLn.Addr(), s.ttl)

	errCh := make(chan error, 2)
	go func() { errCh <- udpServer.ListenAndServe() }()
	go func() { errCh <- tcpServer.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutdownDNSServer(udpServer)
		shutdownDNSServer(tcpServer)
		return nil
	case err := <-errCh:
		shutdownDNSServer(udpServer)
		shutdownDNSServer(tcpServer)
		if err != nil {
			return err
		}
		return nil
	}
}

func shutdownDNSServer(srv *dnsv2.Server) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			_ = recover()
		}()
		srv.Shutdown(context.Background())
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func (s *EmbeddedServer) Sync(ctx context.Context) error {
	zones := map[string]struct{}{}
	records := map[string][]netip.Addr{}
	seenRecords := map[string]map[netip.Addr]struct{}{}

	subnets, err := s.subnets.List(ctx)
	if err != nil {
		return fmt.Errorf("dns embedded list subnets: %w", err)
	}
	domainBySubnet := make(map[string]string, len(subnets))
	for _, sub := range subnets {
		zone, ok := canonicalFQDN(sub.Spec.DomainName)
		if !ok {
			continue
		}
		zones[zone] = struct{}{}
		domainBySubnet[sub.Name] = zone
	}

	machines, err := s.machines.List(ctx)
	if err != nil {
		return fmt.Errorf("dns embedded list machines: %w", err)
	}
	for _, m := range machines {
		domain, ok := canonicalFQDN(m.Network.Domain)
		if !ok {
			domain = domainBySubnet[m.SubnetRef]
			ok = domain != ""
		}
		if !ok {
			continue
		}
		zones[domain] = struct{}{}
		name, ok := hostFQDN(m.Hostname, m.Name, domain)
		if !ok {
			continue
		}
		addRecord(records, seenRecords, name, m.IP)
	}

	vms, err := s.vms.List(ctx)
	if err != nil {
		return fmt.Errorf("dns embedded list virtual machines: %w", err)
	}
	for _, v := range vms {
		domain, ok := canonicalFQDN(v.Domain)
		if !ok {
			domain = domainBySubnet[v.SubnetRef]
			ok = domain != ""
		}
		if !ok {
			continue
		}
		zones[domain] = struct{}{}
		name, ok := hostFQDN(v.Name, v.Name, domain)
		if !ok {
			continue
		}
		for _, ip := range vmIPv4Candidates(v) {
			addRecord(records, seenRecords, name, ip)
		}
	}

	s.mu.Lock()
	if !s.dynamicLoaded {
		if err := s.loadDynamicRecordsLocked(); err != nil {
			s.mu.Unlock()
			return err
		}
		s.dynamicLoaded = true
	}
	s.zones = zones
	s.records = records
	s.serialAt = time.Now().UTC()
	s.mu.Unlock()
	return nil
}

func (s *EmbeddedServer) ServeDNS(ctx context.Context, w dnsv2.ResponseWriter, req *dnsv2.Msg) {
	if req.Opcode == dnsv2.OpcodeUpdate {
		s.serveDynamicUpdate(w, req)
		return
	}

	resp := new(dnsv2.Msg)
	dnsutil.SetReply(resp, req)
	resp.Authoritative = true
	resp.RecursionAvailable = false

	if len(req.Question) != 1 {
		resp.Rcode = dnsv2.RcodeFormatError
		writeDNSResponse(w, resp)
		return
	}

	q := req.Question[0]
	qName := dnsutil.Canonical(q.Header().Name)
	qType := dnsv2.RRToType(q)
	qClass := q.Header().Class

	if qClass != dnsv2.ClassINET && qClass != dnsv2.ClassANY {
		resp.Rcode = dnsv2.RcodeRefused
		writeDNSResponse(w, resp)
		return
	}

	if qType == dnsv2.TypeAXFR {
		s.serveAXFR(ctx, w, req, qName)
		return
	}

	s.mu.RLock()
	zoneMatch := inZone(qName, s.zones)
	answers, knownName := s.queryRecordsLocked(qName, qType)
	s.mu.RUnlock()

	if !zoneMatch {
		resp.Rcode = dnsv2.RcodeRefused
		writeDNSResponse(w, resp)
		return
	}

	if !knownName {
		resp.Rcode = dnsv2.RcodeNameError
		writeDNSResponse(w, resp)
		return
	}

	resp.Answer = answers
	writeDNSResponse(w, resp)
}

func writeDNSResponse(w dnsv2.ResponseWriter, msg *dnsv2.Msg) {
	if err := msg.Pack(); err != nil {
		return
	}
	_, _ = io.Copy(w, msg)
}

func inZone(name string, zones map[string]struct{}) bool {
	for zone := range zones {
		if name == zone || strings.HasSuffix(name, "."+zone) {
			return true
		}
	}
	return false
}

func canonicalFQDN(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	out := dnsutil.Canonical(raw)
	if out == "." || !dnsutil.IsName(out) {
		return "", false
	}
	return out, true
}

func hostFQDN(hostname, fallback, domain string) (string, bool) {
	host := strings.Trim(strings.TrimSpace(hostname), ".")
	if host == "" {
		host = strings.Trim(strings.TrimSpace(fallback), ".")
	}
	if host == "" {
		return "", false
	}

	candidate := dnsutil.Canonical(host)
	if candidate == domain || strings.HasSuffix(candidate, "."+domain) {
		if !dnsutil.IsName(candidate) {
			return "", false
		}
		return candidate, true
	}

	domain = strings.TrimSuffix(domain, ".")
	return canonicalFQDN(host + "." + domain)
}

func addRecord(records map[string][]netip.Addr, seen map[string]map[netip.Addr]struct{}, name, rawIP string) {
	addr, err := netip.ParseAddr(strings.TrimSpace(rawIP))
	if err != nil || !addr.Is4() {
		return
	}
	if seen[name] == nil {
		seen[name] = map[netip.Addr]struct{}{}
	}
	if _, ok := seen[name][addr]; ok {
		return
	}
	seen[name][addr] = struct{}{}
	records[name] = append(records[name], addr)
}

func vmIPv4Candidates(v vm.VirtualMachine) []string {
	out := make([]string, 0, len(v.Network)+len(v.IPAddresses))
	for _, nic := range v.Network {
		out = append(out, nic.IPAddress)
	}
	out = append(out, v.IPAddresses...)
	for _, nic := range v.NetworkInterfaces {
		out = append(out, nic.IPAddresses...)
	}
	return out
}
