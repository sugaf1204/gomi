package dns

import (
	"context"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	dnsv2 "codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnstest"
	"codeberg.org/miekg/dns/rdata"

	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

func TestEmbeddedServerAnswersMachineAndVMARecords(t *testing.T) {
	server := newEmbeddedTestServer(t)

	for _, network := range []string{"udp", "tcp"} {
		t.Run(network, func(t *testing.T) {
			resp := exchangeEmbedded(t, server, network, "node-01.lab.local.", dnsv2.TypeA)
			assertARecord(t, resp, "10.0.0.11")

			resp = exchangeEmbedded(t, server, network, "vm-01.lab.local.", dnsv2.TypeA)
			assertARecord(t, resp, "10.0.0.21")
		})
	}
}

func TestEmbeddedServerRCodes(t *testing.T) {
	server := newEmbeddedTestServer(t)

	resp := serveEmbedded(t, server, "missing.lab.local.", dnsv2.TypeA)
	if resp.Rcode != dnsv2.RcodeNameError {
		t.Fatalf("expected NXDOMAIN, got %s", dnsv2.RcodeToString[resp.Rcode])
	}

	resp = serveEmbedded(t, server, "node-01.example.com.", dnsv2.TypeA)
	if resp.Rcode != dnsv2.RcodeRefused {
		t.Fatalf("expected REFUSED, got %s", dnsv2.RcodeToString[resp.Rcode])
	}

	resp = serveEmbedded(t, server, "node-01.lab.local.", dnsv2.TypeAAAA)
	if resp.Rcode != dnsv2.RcodeSuccess {
		t.Fatalf("expected NOERROR for unsupported type, got %s", dnsv2.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Fatalf("expected empty answer for unsupported type, got %d", len(resp.Answer))
	}
}

func TestEmbeddedServerSyncReplacesSnapshot(t *testing.T) {
	backend := memory.New()
	subnets := backend.Subnets()
	machines := backend.Machines()
	vms := backend.VMs()
	ctx := context.Background()
	now := time.Now().UTC()

	if err := subnets.Upsert(ctx, subnet.Subnet{
		Name:      "lab",
		Spec:      subnet.SubnetSpec{CIDR: "10.0.0.0/24", DomainName: "lab.local"},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := machines.Upsert(ctx, machine.Machine{
		Name:      "node-01",
		Hostname:  "node-01",
		IP:        "10.0.0.11",
		SubnetRef: "lab",
	}); err != nil {
		t.Fatal(err)
	}

	server := NewEmbeddedServer(EmbeddedConfig{
		Addr:     ":0",
		TTL:      300 * time.Second,
		Machines: machines,
		VMs:      vms,
		Subnets:  subnets,
	})
	if err := server.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	assertARecord(t, serveEmbedded(t, server, "node-01.lab.local.", dnsv2.TypeA), "10.0.0.11")

	if err := machines.Upsert(ctx, machine.Machine{
		Name:      "node-01",
		Hostname:  "node-01",
		IP:        "10.0.0.12",
		SubnetRef: "lab",
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	assertARecord(t, serveEmbedded(t, server, "node-01.lab.local.", dnsv2.TypeA), "10.0.0.12")
}

func TestEmbeddedServerAcceptsDynamicUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.json")
	server := newEmbeddedTestServerWithPath(t, path)

	resp := serveEmbeddedUpdate(t, server, []dnsv2.RR{
		&dnsv2.A{
			Hdr: dnsv2.Header{Name: "app.lab.local.", Class: dnsv2.ClassINET, TTL: 60},
			A:   rdata.A{Addr: netip.MustParseAddr("10.0.0.50")},
		},
		&dnsv2.TXT{
			Hdr: dnsv2.Header{Name: "app.lab.local.", Class: dnsv2.ClassINET, TTL: 60},
			TXT: rdata.TXT{Txt: []string{"heritage=external-dns,external-dns/owner=gomi-test"}},
		},
	})
	if resp.Rcode != dnsv2.RcodeSuccess {
		t.Fatalf("expected update success, got %s", dnsv2.RcodeToString[resp.Rcode])
	}
	assertARecord(t, serveEmbedded(t, server, "app.lab.local.", dnsv2.TypeA), "10.0.0.50")
	assertTXTRecord(t, serveEmbedded(t, server, "app.lab.local.", dnsv2.TypeTXT), "heritage=external-dns,external-dns/owner=gomi-test")

	reloaded := newEmbeddedTestServerWithPath(t, path)
	assertARecord(t, serveEmbedded(t, reloaded, "app.lab.local.", dnsv2.TypeA), "10.0.0.50")
}

func TestEmbeddedServerDeletesDynamicRRSet(t *testing.T) {
	server := newEmbeddedTestServerWithPath(t, filepath.Join(t.TempDir(), "records.json"))
	serveEmbeddedUpdate(t, server, []dnsv2.RR{
		&dnsv2.A{
			Hdr: dnsv2.Header{Name: "app.lab.local.", Class: dnsv2.ClassINET, TTL: 60},
			A:   rdata.A{Addr: netip.MustParseAddr("10.0.0.50")},
		},
	})

	resp := serveEmbeddedUpdate(t, server, []dnsv2.RR{
		&dnsv2.A{Hdr: dnsv2.Header{Name: "app.lab.local.", Class: dnsv2.ClassANY, TTL: 0}},
	})
	if resp.Rcode != dnsv2.RcodeSuccess {
		t.Fatalf("expected delete success, got %s", dnsv2.RcodeToString[resp.Rcode])
	}
	resp = serveEmbedded(t, server, "app.lab.local.", dnsv2.TypeA)
	if resp.Rcode != dnsv2.RcodeNameError {
		t.Fatalf("expected NXDOMAIN after delete, got %s", dnsv2.RcodeToString[resp.Rcode])
	}
}

func TestEmbeddedServerAXFRIncludesDynamicRecords(t *testing.T) {
	server := newEmbeddedTestServerWithPath(t, filepath.Join(t.TempDir(), "records.json"))
	serveEmbeddedUpdate(t, server, []dnsv2.RR{
		&dnsv2.A{
			Hdr: dnsv2.Header{Name: "app.lab.local.", Class: dnsv2.ClassINET, TTL: 60},
			A:   rdata.A{Addr: netip.MustParseAddr("10.0.0.50")},
		},
	})

	cancel, addr, err := dnstest.TCPServer(":0", func(s *dnsv2.Server) {
		s.Handler = server
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	req := dnsv2.NewMsg("lab.local.", dnsv2.TypeAXFR)
	client := &dnsv2.Client{Transport: dnsv2.NewTransport()}
	env, err := client.TransferIn(context.Background(), req, "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for msg := range env {
		if msg.Error != nil {
			t.Fatal(msg.Error)
		}
		for _, rr := range msg.Answer {
			a, ok := rr.(*dnsv2.A)
			if ok && a.Hdr.Name == "app.lab.local." && a.A.Addr.String() == "10.0.0.50" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected AXFR to include dynamic A record")
	}
}

func newEmbeddedTestServer(t *testing.T) *EmbeddedServer {
	t.Helper()
	return newEmbeddedTestServerWithPath(t, "")
}

func newEmbeddedTestServerWithPath(t *testing.T, dynamicRecordsPath string) *EmbeddedServer {
	t.Helper()

	backend := memory.New()
	subnets := backend.Subnets()
	machines := backend.Machines()
	vms := backend.VMs()
	ctx := context.Background()
	now := time.Now().UTC()

	if err := subnets.Upsert(ctx, subnet.Subnet{
		Name:      "lab",
		Spec:      subnet.SubnetSpec{CIDR: "10.0.0.0/24", DomainName: "lab.local"},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := machines.Upsert(ctx, machine.Machine{
		Name:      "node-01",
		Hostname:  "node-01",
		IP:        "10.0.0.11",
		SubnetRef: "lab",
	}); err != nil {
		t.Fatal(err)
	}
	if err := vms.Upsert(ctx, vm.VirtualMachine{
		Name:      "vm-01",
		SubnetRef: "lab",
		Network: []vm.NetworkInterface{
			{Name: "default", IPAddress: "10.0.0.21"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	server := NewEmbeddedServer(EmbeddedConfig{
		Addr:               ":0",
		TTL:                300 * time.Second,
		DynamicRecordsPath: dynamicRecordsPath,
		Machines:           machines,
		VMs:                vms,
		Subnets:            subnets,
	})
	if err := server.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	return server
}

func exchangeEmbedded(t *testing.T, server *EmbeddedServer, network, name string, qtype uint16) *dnsv2.Msg {
	t.Helper()

	var run func(string, ...func(*dnsv2.Server)) (func(), string, error)
	switch network {
	case "udp":
		run = dnstest.UDPServer
	case "tcp":
		run = dnstest.TCPServer
	default:
		t.Fatalf("unsupported network: %s", network)
	}

	cancel, addr, err := run(":0", func(s *dnsv2.Server) {
		s.Handler = server
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	req := dnsv2.NewMsg(name, qtype)
	if req == nil {
		t.Fatalf("failed to create query %s type %d", name, qtype)
	}
	if err := req.Pack(); err != nil {
		t.Fatal(err)
	}

	client := &dnsv2.Client{Transport: dnsv2.NewTransport()}
	resp, _, err := client.Exchange(context.Background(), req, network, addr)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func serveEmbedded(t *testing.T, server *EmbeddedServer, name string, qtype uint16) *dnsv2.Msg {
	t.Helper()

	req := dnsv2.NewMsg(name, qtype)
	if req == nil {
		t.Fatalf("failed to create query %s type %d", name, qtype)
	}
	rec := dnstest.NewTestRecorder()
	server.ServeDNS(context.Background(), rec, req)
	if rec.Msg == nil {
		t.Fatal("expected DNS response")
	}
	if err := rec.Msg.Unpack(); err != nil {
		t.Fatal(err)
	}
	return rec.Msg
}

func serveEmbeddedUpdate(t *testing.T, server *EmbeddedServer, updates []dnsv2.RR) *dnsv2.Msg {
	t.Helper()

	req := dnsv2.NewMsg("lab.local.", dnsv2.TypeSOA)
	req.Opcode = dnsv2.OpcodeUpdate
	req.Ns = updates
	rec := dnstest.NewTestRecorder()
	server.ServeDNS(context.Background(), rec, req)
	if rec.Msg == nil {
		t.Fatal("expected DNS update response")
	}
	if err := rec.Msg.Unpack(); err != nil {
		t.Fatal(err)
	}
	return rec.Msg
}

func assertARecord(t *testing.T, resp *dnsv2.Msg, want string) {
	t.Helper()

	if resp.Rcode != dnsv2.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dnsv2.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dnsv2.A)
	if !ok {
		t.Fatalf("expected A answer, got %T", resp.Answer[0])
	}
	if got := a.A.Addr.String(); got != want {
		t.Fatalf("expected A %s, got %s", want, got)
	}
}

func TestEmbeddedServerResolvesVMWithDirectDomain(t *testing.T) {
	backend := memory.New()
	ctx := context.Background()
	now := time.Now().UTC()

	if err := backend.Subnets().Upsert(ctx, subnet.Subnet{
		Name:      "lab",
		Spec:      subnet.SubnetSpec{CIDR: "10.0.0.0/24"},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.VMs().Upsert(ctx, vm.VirtualMachine{
		Name:   "vm-direct",
		Domain: "corp.example",
		Network: []vm.NetworkInterface{
			{Name: "default", IPAddress: "10.0.0.30"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	server := NewEmbeddedServer(EmbeddedConfig{
		Addr:     ":0",
		TTL:      300 * time.Second,
		Machines: backend.Machines(),
		VMs:      backend.VMs(),
		Subnets:  backend.Subnets(),
	})
	if err := server.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	assertARecord(t, serveEmbedded(t, server, "vm-direct.corp.example.", dnsv2.TypeA), "10.0.0.30")

	resp := serveEmbedded(t, server, "vm-direct.lab.local.", dnsv2.TypeA)
	if resp.Rcode != dnsv2.RcodeRefused {
		t.Fatalf("expected REFUSED for wrong zone, got %s", dnsv2.RcodeToString[resp.Rcode])
	}
}

func assertTXTRecord(t *testing.T, resp *dnsv2.Msg, want string) {
	t.Helper()

	if resp.Rcode != dnsv2.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dnsv2.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	txt, ok := resp.Answer[0].(*dnsv2.TXT)
	if !ok {
		t.Fatalf("expected TXT answer, got %T", resp.Answer[0])
	}
	if got := txt.TXT.Txt[0]; got != want {
		t.Fatalf("expected TXT %q, got %q", want, got)
	}
}
