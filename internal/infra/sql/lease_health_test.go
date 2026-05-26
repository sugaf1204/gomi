package sql_test

import (
	"context"
	"github.com/sugaf1204/gomi/internal/pxe"
	"testing"
	"time"
)

func TestDHCPLeaseStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.DHCPLeases()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	lease := pxe.DHCPLease{
		MAC: "AA:BB:CC:DD:EE:01", IP: "10.0.0.100",
		Hostname: "srv1", PXEClient: true, LeasedAt: now,
	}
	if err := s.Upsert(ctx, lease); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || !list[0].PXEClient {
		t.Errorf("unexpected lease list: %+v", list)
	}

	if err := s.Delete(ctx, "AA:BB:CC:DD:EE:01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ = s.List(ctx)
	if len(list) != 0 {
		t.Errorf("after delete len = %d", len(list))
	}
}

func TestHealth(t *testing.T) {
	b := newTestBackend(t)
	if err := b.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}
