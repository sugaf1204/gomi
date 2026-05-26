package memory_test

import (
	"context"
	"errors"
	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
	"testing"
	"time"
)

func TestSessionExpiry(t *testing.T) {
	b := memory.New()
	store := b.Auth()
	ctx := context.Background()

	session := auth.Session{
		Token:     "expired-token",
		Username:  "admin",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, err := store.GetSession(ctx, "expired-token")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for expired session, got %v", err)
	}
}

func TestAuditCaseInsensitiveFilter(t *testing.T) {
	b := memory.New()
	store := b.Auth()
	ctx := context.Background()

	event := auth.AuditEvent{
		ID:        "evt-1",
		Machine:   "Node-01",
		Action:    "create-machine",
		Actor:     "admin",
		Result:    "success",
		CreatedAt: time.Now(),
	}
	if err := store.CreateAuditEvent(ctx, event); err != nil {
		t.Fatalf("CreateAuditEvent: %v", err)
	}

	events, err := store.ListAuditEvents(ctx, "node-01", 0)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event with case-insensitive filter, got %d", len(events))
	}
	if events[0].ID != "evt-1" {
		t.Fatalf("expected event evt-1, got %s", events[0].ID)
	}
}

// --- Backend stores initialization ---

func TestBackendStoresInitialized(t *testing.T) {
	b := memory.New()

	if b.Hypervisors() == nil {
		t.Fatal("expected non-nil HypervisorStore")
	}
	if b.HypervisorTokens() == nil {
		t.Fatal("expected non-nil RegTokenStore")
	}
	if b.VMs() == nil {
		t.Fatal("expected non-nil VMStore")
	}
	if b.CloudInits() == nil {
		t.Fatal("expected non-nil CloudInitStore")
	}
	if b.OSImages() == nil {
		t.Fatal("expected non-nil OSImageStore")
	}
}

func TestBackendHealthAndClose(t *testing.T) {
	b := memory.New()
	ctx := context.Background()

	if err := b.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSubnetStoreChangeNotifier(t *testing.T) {
	b := memory.New()
	store := b.Subnets()
	ctx := context.Background()
	called := make(chan struct{}, 2)

	store.Subscribe(func() { called <- struct{}{} })

	if err := store.Upsert(ctx, subnet.Subnet{
		Name: "lab",
		Spec: subnet.SubnetSpec{CIDR: "192.168.2.0/24"},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	waitForSubnetNotification(t, called)

	if err := store.Delete(ctx, "lab"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	waitForSubnetNotification(t, called)
}

func waitForSubnetNotification(t *testing.T, called <-chan struct{}) {
	t.Helper()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subnet change notification")
	}
}

// --- HypervisorStore ---
