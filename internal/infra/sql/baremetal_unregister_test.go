package sql_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sugaf1204/gomi/internal/baremetal"
	"github.com/sugaf1204/gomi/internal/machine"
)

func TestBareMetalUnregisterRequiresUnownedHostAndCurrentRevision(t *testing.T) {
	ctx := context.Background()
	b := physicalBackend(t, filepath.Join(t.TempDir(), "gomi.db"))
	s := b.BareMetal()
	h := registerHost(t, b, "node1", "pool")
	if err := s.Unregister(ctx, h.Name, h.Revision+1); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("accepted stale revision: %v", err)
	}
	claimed, err := s.Acquire(ctx, "pool", "capi-owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unregister(ctx, h.Name, claimed.Revision); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("removed an allocated host: %v", err)
	}
	releasing, err := s.Transition(ctx, claimed, baremetal.Releasing, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unregister(ctx, h.Name, releasing.Revision); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("removed a releasing host: %v", err)
	}
	released, err := s.CompleteRelease(ctx, releasing)
	if err != nil {
		t.Fatal(err)
	}
	m, err := b.Machines().Get(ctx, h.Name)
	if err != nil {
		t.Fatal(err)
	}
	m.SealedBootstrap = &machine.SealedBootstrap{Owner: "capi-owner", Cleanup: true}
	m.Provision = &machine.ProvisionProgress{Active: true}
	if err := b.Machines().Upsert(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.Unregister(ctx, h.Name, released.Revision); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("removed a provisioning host: %v", err)
	}
	if _, err := s.Get(ctx, h.Name); err != nil {
		t.Fatal("failed unregister did not roll back enrollment", err)
	}
	m.Provision.Active = false
	if err := b.Machines().Upsert(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.Unregister(ctx, h.Name, h.Revision); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("accepted pre-claim revision after release: %v", err)
	}
	if err := s.Unregister(ctx, h.Name, released.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, h.Name); !errors.Is(err, baremetal.ErrNotFound) {
		t.Fatalf("enrollment remains: %v", err)
	}
	m, err = b.Machines().Get(ctx, h.Name)
	if err != nil {
		t.Fatalf("ordinary machine inventory was removed: %v", err)
	}
	if m.SealedBootstrap != nil {
		t.Fatal("sealed cleanup leaked into ordinary machine deployment")
	}
}
