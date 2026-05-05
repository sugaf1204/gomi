package dns

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	dnsv2 "codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
	"codeberg.org/miekg/dns/rdata"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

var _ Controller = (*RFC2136Controller)(nil)

type RFC2136Config struct {
	Server        string
	Zone          string
	TTL           time.Duration
	TSIGName      string
	TSIGSecret    string
	TSIGAlgorithm string
	Transport     string
	Machines      machine.Store
	VMs           vm.Store
	Subnets       subnet.Store
}

type RFC2136Controller struct {
	server        string
	zone          string
	ttl           uint32
	tsigName      string
	tsigSecret    string
	tsigAlgorithm string
	transport     string
	machines      machine.Store
	vms           vm.Store
	subnets       subnet.Store
	client        *dnsv2.Client

	mu   sync.Mutex
	last map[string]recordSet
}

type recordSet struct {
	Zone string
	Name string
	IPs  []netip.Addr
}

func NewRFC2136Controller(cfg RFC2136Config) *RFC2136Controller {
	ttl := uint32(300)
	if cfg.TTL > 0 {
		ttl = uint32(cfg.TTL.Seconds())
	}
	transport := strings.ToLower(strings.TrimSpace(cfg.Transport))
	if transport == "" {
		transport = "udp"
	}
	algorithm := dnsutil.Canonical(strings.TrimSpace(cfg.TSIGAlgorithm))
	if algorithm == "." {
		algorithm = dnsv2.HmacSHA256
	}
	return &RFC2136Controller{
		server:        dnsServerAddress(cfg.Server),
		zone:          canonicalOrEmpty(cfg.Zone),
		ttl:           ttl,
		tsigName:      canonicalOrEmpty(cfg.TSIGName),
		tsigSecret:    strings.TrimSpace(cfg.TSIGSecret),
		tsigAlgorithm: algorithm,
		transport:     transport,
		machines:      cfg.Machines,
		vms:           cfg.VMs,
		subnets:       cfg.Subnets,
		client:        dnsv2.NewClient(),
		last:          map[string]recordSet{},
	}
}

func (c *RFC2136Controller) Start(ctx context.Context) error {
	if err := c.Sync(ctx); err != nil {
		return err
	}
	if c.Enabled() {
		log.Printf("dns: rfc2136 dynamic update enabled server=%s zone=%s transport=%s ttl=%ds", c.server, c.zone, c.transport, c.ttl)
	}
	<-ctx.Done()
	return nil
}

func (c *RFC2136Controller) Enabled() bool {
	return c.server != "" && c.machines != nil && c.vms != nil && c.subnets != nil
}

func (c *RFC2136Controller) Sync(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}
	if c.transport != "udp" && c.transport != "tcp" {
		return fmt.Errorf("rfc2136 transport must be udp or tcp, got %q", c.transport)
	}

	desired, err := c.collectRecords(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for key, rec := range desired {
		if equalRecordSet(c.last[key], rec) {
			continue
		}
		if err := c.replaceA(ctx, rec); err != nil {
			return err
		}
		c.last[key] = rec
	}
	for key, rec := range c.last {
		if _, ok := desired[key]; ok {
			continue
		}
		if err := c.deleteA(ctx, rec); err != nil {
			return err
		}
		delete(c.last, key)
	}
	return nil
}

func (c *RFC2136Controller) collectRecords(ctx context.Context) (map[string]recordSet, error) {
	subnets, err := c.subnets.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("rfc2136 list subnets: %w", err)
	}
	domainBySubnet := make(map[string]string, len(subnets))
	for _, sub := range subnets {
		if domain, ok := canonicalFQDN(sub.Spec.DomainName); ok {
			domainBySubnet[sub.Name] = domain
		}
	}

	records := map[string]recordSet{}
	seen := map[string]map[netip.Addr]struct{}{}

	machines, err := c.machines.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("rfc2136 list machines: %w", err)
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
		name, ok := hostFQDN(m.Hostname, m.Name, domain)
		if !ok {
			continue
		}
		c.addRecord(records, seen, name, domain, m.IP)
	}

	vms, err := c.vms.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("rfc2136 list virtual machines: %w", err)
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
		name, ok := hostFQDN(v.Name, v.Name, domain)
		if !ok {
			continue
		}
		for _, ip := range vmIPv4Candidates(v) {
			c.addRecord(records, seen, name, domain, ip)
		}
	}
	return records, nil
}

func (c *RFC2136Controller) addRecord(records map[string]recordSet, seen map[string]map[netip.Addr]struct{}, name, domain, rawIP string) {
	addr, err := netip.ParseAddr(strings.TrimSpace(rawIP))
	if err != nil || !addr.Is4() {
		return
	}
	zone := domain
	if c.zone != "" {
		if !inZone(name, map[string]struct{}{c.zone: {}}) {
			return
		}
		zone = c.zone
	}
	if seen[name] == nil {
		seen[name] = map[netip.Addr]struct{}{}
	}
	if _, ok := seen[name][addr]; ok {
		return
	}
	seen[name][addr] = struct{}{}
	rec := records[name]
	if rec.Name == "" {
		rec = recordSet{Name: name, Zone: zone}
	}
	rec.IPs = append(rec.IPs, addr)
	records[name] = rec
}

func (c *RFC2136Controller) replaceA(ctx context.Context, rec recordSet) error {
	msg := c.newUpdate(rec.Zone)
	msg.Ns = append(msg.Ns, deleteARRSet(rec.Name))
	for _, ip := range rec.IPs {
		msg.Ns = append(msg.Ns, &dnsv2.A{
			Hdr: dnsv2.Header{Name: rec.Name, Class: dnsv2.ClassINET, TTL: c.ttl},
			A:   rdata.A{Addr: ip},
		})
	}
	return c.exchangeUpdate(ctx, msg, rec.Name)
}

func (c *RFC2136Controller) deleteA(ctx context.Context, rec recordSet) error {
	msg := c.newUpdate(rec.Zone)
	msg.Ns = append(msg.Ns, deleteARRSet(rec.Name))
	return c.exchangeUpdate(ctx, msg, rec.Name)
}

func (c *RFC2136Controller) newUpdate(zone string) *dnsv2.Msg {
	msg := dnsv2.NewMsg(zone, dnsv2.TypeSOA)
	msg.Opcode = dnsv2.OpcodeUpdate
	return msg
}

func (c *RFC2136Controller) exchangeUpdate(ctx context.Context, msg *dnsv2.Msg, name string) error {
	var tsigOpt dnsv2.TSIGOption
	var signer dnsv2.TSIGSigner
	if c.tsigName != "" || c.tsigSecret != "" {
		if c.tsigName == "" || c.tsigSecret == "" {
			return fmt.Errorf("rfc2136 tsig_name and tsig_secret must be configured together")
		}
		secret, err := base64.StdEncoding.DecodeString(c.tsigSecret)
		if err != nil {
			return fmt.Errorf("rfc2136 tsig_secret must be base64: %w", err)
		}
		signer = dnsv2.HmacTSIG{Secret: secret}
		msg.Pseudo = append(msg.Pseudo, dnsv2.NewTSIG(c.tsigName, c.tsigAlgorithm, 0))
		if err := dnsv2.TSIGSign(msg, signer, &tsigOpt); err != nil {
			return fmt.Errorf("rfc2136 sign update %s: %w", name, err)
		}
	}

	resp, _, err := c.client.Exchange(ctx, msg, c.transport, c.server)
	if err != nil {
		return fmt.Errorf("rfc2136 update %s: %w", name, err)
	}
	if signer != nil {
		if err := dnsv2.TSIGVerify(resp, signer, &tsigOpt); err != nil {
			return fmt.Errorf("rfc2136 verify response %s: %w", name, err)
		}
	}
	if resp.Rcode != dnsv2.RcodeSuccess {
		return fmt.Errorf("rfc2136 update %s returned %s", name, dnsv2.RcodeToString[resp.Rcode])
	}
	return nil
}

func deleteARRSet(name string) *dnsv2.RFC3597 {
	return &dnsv2.RFC3597{
		Hdr:     dnsv2.Header{Name: name, Class: dnsv2.ClassANY, TTL: 0},
		RFC3597: rdata.RFC3597{RRType: dnsv2.TypeA},
	}
}

func equalRecordSet(a, b recordSet) bool {
	if a.Name != b.Name || a.Zone != b.Zone || len(a.IPs) != len(b.IPs) {
		return false
	}
	for i := range a.IPs {
		if a.IPs[i] != b.IPs[i] {
			return false
		}
	}
	return true
}

func dnsServerAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(raw); err == nil {
		return raw
	}
	if strings.Count(raw, ":") == 0 {
		return net.JoinHostPort(raw, "53")
	}
	return raw
}

func canonicalOrEmpty(raw string) string {
	if out, ok := canonicalFQDN(raw); ok {
		return out
	}
	return ""
}
