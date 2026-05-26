package dns

import (
	dnsv2 "codeberg.org/miekg/dns"
	"context"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
	"testing"
	"time"
)

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
