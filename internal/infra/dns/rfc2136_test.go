package dns

import (
	"context"
	"sync"
	"testing"
	"time"

	dnsv2 "codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnstest"
	"codeberg.org/miekg/dns/dnsutil"

	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

func TestRFC2136ControllerSyncsMachineAndVMRecords(t *testing.T) {
	var mu sync.Mutex
	var updates []*dnsv2.Msg
	cancel, addr, err := dnstest.UDPServer(":0", func(s *dnsv2.Server) {
		s.Handler = dnsv2.HandlerFunc(func(_ context.Context, w dnsv2.ResponseWriter, req *dnsv2.Msg) {
			req.Options = dnsv2.MsgOptionUnpack
			if err := req.Unpack(); err != nil {
				t.Errorf("unpack update: %v", err)
			}
			mu.Lock()
			updates = append(updates, cloneUpdateMsg(req))
			mu.Unlock()
			resp := new(dnsv2.Msg)
			dnsutil.SetReply(resp, req)
			resp.Rcode = dnsv2.RcodeSuccess
			writeDNSResponse(w, resp)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	backend := memory.New()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := backend.Subnets().Upsert(ctx, subnet.Subnet{
		Name:      "lab",
		Spec:      subnet.SubnetSpec{CIDR: "10.0.0.0/24", DomainName: "lab.local"},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Machines().Upsert(ctx, machine.Machine{
		Name:      "node-01",
		Hostname:  "node-01",
		IP:        "10.0.0.11",
		SubnetRef: "lab",
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.VMs().Upsert(ctx, vm.VirtualMachine{
		Name:      "vm-01",
		SubnetRef: "lab",
		Network: []vm.NetworkInterface{
			{Name: "default", IPAddress: "10.0.0.21"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	controller := NewRFC2136Controller(RFC2136Config{
		Server:   addr,
		Zone:     "lab.local",
		TTL:      45 * time.Second,
		Machines: backend.Machines(),
		VMs:      backend.VMs(),
		Subnets:  backend.Subnets(),
	})
	if err := controller.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := controller.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(updates) != 2 {
		t.Fatalf("expected 2 initial updates and no duplicate second sync updates, got %d", len(updates))
	}

	got := map[string]string{}
	for _, msg := range updates {
		if msg.Opcode != dnsv2.OpcodeUpdate {
			t.Fatalf("expected UPDATE opcode, got %s", dnsv2.OpcodeToString[msg.Opcode])
		}
		if len(msg.Question) != 1 || msg.Question[0].Header().Name != "lab.local." {
			t.Fatalf("unexpected zone section: %#v", msg.Question)
		}
		if len(msg.Ns) != 2 {
			t.Fatalf("expected delete+insert records, got %d", len(msg.Ns))
		}
		del, ok := msg.Ns[0].(*dnsv2.A)
		if !ok || del.Hdr.Class != dnsv2.ClassANY || del.Hdr.TTL != 0 {
			t.Fatalf("expected first update to delete existing A rrset, got %#v", msg.Ns[0])
		}
		add, ok := msg.Ns[1].(*dnsv2.A)
		if !ok || add.Hdr.Class != dnsv2.ClassINET || add.Hdr.TTL != 45 {
			t.Fatalf("expected second update to insert A record, got %#v", msg.Ns[1])
		}
		got[add.Hdr.Name] = add.A.Addr.String()
	}
	if got["node-01.lab.local."] != "10.0.0.11" || got["vm-01.lab.local."] != "10.0.0.21" {
		t.Fatalf("unexpected updates: %#v", got)
	}
}

func TestRFC2136ControllerSyncsVMWithDirectDomain(t *testing.T) {
	var mu sync.Mutex
	var updates []*dnsv2.Msg
	cancel, addr, err := dnstest.UDPServer(":0", func(s *dnsv2.Server) {
		s.Handler = dnsv2.HandlerFunc(func(_ context.Context, w dnsv2.ResponseWriter, req *dnsv2.Msg) {
			req.Options = dnsv2.MsgOptionUnpack
			if err := req.Unpack(); err != nil {
				t.Errorf("unpack update: %v", err)
			}
			mu.Lock()
			updates = append(updates, cloneUpdateMsg(req))
			mu.Unlock()
			resp := new(dnsv2.Msg)
			dnsutil.SetReply(resp, req)
			resp.Rcode = dnsv2.RcodeSuccess
			writeDNSResponse(w, resp)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

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

	controller := NewRFC2136Controller(RFC2136Config{
		Server:   addr,
		Zone:     "corp.example",
		TTL:      45 * time.Second,
		Machines: backend.Machines(),
		VMs:      backend.VMs(),
		Subnets:  backend.Subnets(),
	})
	if err := controller.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(updates) != 1 {
		t.Fatalf("expected 1 update for vm with direct domain, got %d", len(updates))
	}
	add, ok := updates[0].Ns[1].(*dnsv2.A)
	if !ok {
		t.Fatalf("expected A record insert, got %T", updates[0].Ns[1])
	}
	if got := add.A.Addr.String(); got != "10.0.0.30" {
		t.Fatalf("expected 10.0.0.30, got %s", got)
	}
	if got := add.Hdr.Name; got != "vm-direct.corp.example." {
		t.Fatalf("expected vm-direct.corp.example., got %s", got)
	}
}

func cloneUpdateMsg(msg *dnsv2.Msg) *dnsv2.Msg {
	return msg.Copy()
}
