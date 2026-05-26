package memory_test

import (
	"context"
	"errors"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/resource"
	"testing"
	"time"
)

func TestHypervisorStoreCRUD(t *testing.T) {
	b := memory.New()
	store := b.Hypervisors()
	ctx := context.Background()

	h := hypervisor.Hypervisor{
		Name: "hv-01",
	}
	if err := store.Upsert(ctx, h); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get(ctx, "hv-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "hv-01" {
		t.Fatalf("expected hv-01, got %s", got.Name)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	if err := store.Delete(ctx, "hv-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "hv-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestHypervisorStoreDeleteNonExistent(t *testing.T) {
	b := memory.New()
	store := b.Hypervisors()
	ctx := context.Background()

	err := store.Delete(ctx, "ghost")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- RegTokenStore ---

func TestRegTokenStoreCreateAndGet(t *testing.T) {
	b := memory.New()
	store := b.HypervisorTokens()
	ctx := context.Background()

	token := hypervisor.RegistrationToken{
		Token: "test-token-123",
	}
	if err := store.Create(ctx, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, "test-token-123")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Token != "test-token-123" {
		t.Fatalf("expected token test-token-123, got %s", got.Token)
	}
}

func TestRegTokenStoreGetNonExistent(t *testing.T) {
	b := memory.New()
	store := b.HypervisorTokens()
	ctx := context.Background()

	_, err := store.Get(ctx, "non-existent")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRegTokenStoreMarkUsed(t *testing.T) {
	b := memory.New()
	store := b.HypervisorTokens()
	ctx := context.Background()

	token := hypervisor.RegistrationToken{
		Token:     "use-me-token",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Create(ctx, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.MarkUsed(ctx, "use-me-token", "hv-registered")
	if err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	if !got.Used {
		t.Fatal("expected Used=true after MarkUsed")
	}
	if got.UsedBy != "hv-registered" {
		t.Fatalf("expected UsedBy=hv-registered, got %s", got.UsedBy)
	}
}

func TestRegTokenStoreMarkUsedNonExistent(t *testing.T) {
	b := memory.New()
	store := b.HypervisorTokens()
	ctx := context.Background()

	_, err := store.MarkUsed(ctx, "ghost-token", "hv")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRegTokenStoreList(t *testing.T) {
	b := memory.New()
	store := b.HypervisorTokens()
	ctx := context.Background()

	tokens := []hypervisor.RegistrationToken{
		{Token: "token-1", CreatedAt: time.Now()},
		{Token: "token-2", CreatedAt: time.Now().Add(time.Second)},
		{Token: "token-3", CreatedAt: time.Now().Add(2 * time.Second)},
	}
	for _, tok := range tokens {
		if err := store.Create(ctx, tok); err != nil {
			t.Fatalf("Create %s: %v", tok.Token, err)
		}
	}

	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 tokens total, got %d", len(all))
	}
}

// --- VMStore ---
