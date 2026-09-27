package sql_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sugaf1204/gomi/internal/baremetal"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/machine"
)

func physicalBackend(t *testing.T, path string) *infrasql.Backend {
	t.Helper()
	b, err := infrasql.New("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}
func registerHost(t *testing.T, b *infrasql.Backend, name, pool string) baremetal.Host {
	t.Helper()
	ctx := context.Background()
	if err := b.Machines().Upsert(ctx, machine.Machine{Name: name, MAC: name}); err != nil {
		t.Fatal(err)
	}
	h, err := b.BareMetal().Register(ctx, name, pool, testPublicKey(t), "/dev/nvme0n1")
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func TestBareMetalPoolUpdateRequiresAvailableHostAndCurrentRevision(t *testing.T) {
	ctx := context.Background()
	b := physicalBackend(t, filepath.Join(t.TempDir(), "gomi.db"))
	s := b.BareMetal()
	h := registerHost(t, b, "node1", "cluster02")

	updated, err := s.UpdatePool(ctx, h.Name, "cilium-cluster", h.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Pool != "cilium-cluster" || updated.Revision != h.Revision+1 {
		t.Fatalf("unexpected update: %#v", updated)
	}
	if _, err = s.UpdatePool(ctx, h.Name, "stale", h.Revision); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	claimed, err := s.Acquire(ctx, updated.Pool, "capi-owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdatePool(ctx, h.Name, "other", claimed.Revision); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("allocated host moved pools: %v", err)
	}
}
func TestBareMetalClaimLifecycle(t *testing.T) {
	ctx := context.Background()
	b := physicalBackend(t, filepath.Join(t.TempDir(), "gomi.db"))
	s := b.BareMetal()
	registerHost(t, b, "node1", "cluster02")
	h, err := s.Acquire(ctx, "cluster02", "capi-owner")
	if err != nil {
		t.Fatal(err)
	}
	original := h
	if _, err = s.Register(ctx, "node1", "other", testPublicKey(t), "/dev/nvme0n1"); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("pool reassignment: %v", err)
	}
	again, err := s.Register(ctx, "node1", "cluster02", testPublicKey(t), "/dev/nvme0n1")
	if err != nil || again != h {
		t.Fatalf("register reset a claim: %#v %v", again, err)
	}
	if _, err = s.Acquire(ctx, "other", "capi-owner"); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("owner moved pools: %v", err)
	}
	if _, err = s.CompleteRelease(ctx, h); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("released before cleanup: %v", err)
	}
	h, err = s.Transition(ctx, h, baremetal.Deploying, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Transition(ctx, original, baremetal.Releasing, ""); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("stale transition: %v", err)
	}
	if _, err = s.Transition(ctx, h, baremetal.Ready, "attempt-2"); err == nil {
		t.Fatal("attempt identity changed")
	}
	h, err = s.Transition(ctx, h, baremetal.Ready, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	h, err = s.Transition(ctx, h, baremetal.Releasing, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Acquire(ctx, "cluster02", "capi-new"); !errors.Is(err, baremetal.ErrCapacity) {
		t.Fatalf("allocated before release: %v", err)
	}
	stale := h
	h, err = s.CompleteRelease(ctx, h)
	if err != nil || h.Owner != "" || h.AttemptID != "" {
		t.Fatalf("release: %#v %v", h, err)
	}
	h, err = s.Acquire(ctx, "cluster02", "capi-new")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteRelease(ctx, stale); !errors.Is(err, baremetal.ErrConflict) {
		t.Fatalf("old owner released new claim: %v", err)
	}
}
func TestBareMetalIndependentServersAndRecovery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "gomi.db")
	first := physicalBackend(t, path)
	second := physicalBackend(t, path)
	for i := 0; i < 3; i++ {
		registerHost(t, first, fmt.Sprintf("node%d", i), "cluster02")
	}
	var wg sync.WaitGroup
	results := make(chan baremetal.Host, 24)
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := first.BareMetal()
			if i%2 == 0 {
				s = second.BareMetal()
			}
			h, e := s.Acquire(ctx, "cluster02", fmt.Sprintf("capi-owner-%d", i))
			if e == nil {
				results <- h
			} else {
				errs <- e
			}
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	names := map[string]bool{}
	for h := range results {
		if names[h.Name] {
			t.Fatalf("duplicate allocation: %s", h.Name)
		}
		names[h.Name] = true
	}
	if len(names) != 3 {
		t.Fatalf("allocated %d hosts, want 3", len(names))
	}
	for err := range errs {
		if !errors.Is(err, baremetal.ErrCapacity) {
			t.Fatal(err)
		}
	}
	reopened := physicalBackend(t, path)
	h, err := first.BareMetal().Get(ctx, "node0")
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.BareMetal().Acquire(ctx, "cluster02", h.Owner)
	if err != nil || recovered != h {
		t.Fatalf("lost persistent identity: %#v %v", recovered, err)
	}
}
func TestBareMetalConcurrentSameOwner(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "gomi.db")
	b := physicalBackend(t, path)
	other := physicalBackend(t, path)
	for i := 0; i < 3; i++ {
		registerHost(t, b, fmt.Sprintf("node%d", i), "pool")
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := b.BareMetal()
			if i%2 == 0 {
				s = other.BareMetal()
			}
			h, err := s.Acquire(ctx, "pool", "capi-same")
			if err != nil || h.Name != "node0" {
				t.Errorf("same-owner retry: %#v %v", h, err)
			}
		}(i)
	}
	wg.Wait()
	h, err := b.BareMetal().Acquire(ctx, "pool", "capi-second")
	if err != nil || h.Name != "node1" {
		t.Fatalf("retry consumed multiple hosts: %#v %v", h, err)
	}
}

func testPublicKey(t *testing.T) string {
	t.Helper()
	key, err := os.ReadFile("../../baremetal/testdata/host-public.pem")
	if err != nil {
		t.Fatal(err)
	}
	return string(key)
}
