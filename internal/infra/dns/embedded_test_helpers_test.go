package dns

import (
	dnsv2 "codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnstest"
	"context"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
	"testing"
	"time"
)

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
