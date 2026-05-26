package memory_test

import (
	"context"
	"errors"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
	"testing"
)

func TestVMStoreCRUD(t *testing.T) {
	b := memory.New()
	store := b.VMs()
	ctx := context.Background()

	v := vm.VirtualMachine{
		Name:          "vm-01",
		HypervisorRef: "hv-01",
	}
	if err := store.Upsert(ctx, v); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get(ctx, "vm-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "vm-01" {
		t.Fatalf("expected vm-01, got %s", got.Name)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	if err := store.Delete(ctx, "vm-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "vm-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestVMStoreListByHypervisor(t *testing.T) {
	b := memory.New()
	store := b.VMs()
	ctx := context.Background()

	vms := []vm.VirtualMachine{
		{Name: "vm-a", HypervisorRef: "hv-01"},
		{Name: "vm-b", HypervisorRef: "hv-01"},
		{Name: "vm-c", HypervisorRef: "hv-02"},
	}
	for _, v := range vms {
		if err := store.Upsert(ctx, v); err != nil {
			t.Fatalf("Upsert %s: %v", v.Name, err)
		}
	}

	hv01List, err := store.ListByHypervisor(ctx, "hv-01")
	if err != nil {
		t.Fatalf("ListByHypervisor hv-01: %v", err)
	}
	if len(hv01List) != 2 {
		t.Fatalf("expected 2 VMs on hv-01, got %d", len(hv01List))
	}

	hv02List, err := store.ListByHypervisor(ctx, "hv-02")
	if err != nil {
		t.Fatalf("ListByHypervisor hv-02: %v", err)
	}
	if len(hv02List) != 1 {
		t.Fatalf("expected 1 VM on hv-02, got %d", len(hv02List))
	}

	emptyList, err := store.ListByHypervisor(ctx, "hv-99")
	if err != nil {
		t.Fatalf("ListByHypervisor hv-99: %v", err)
	}
	if len(emptyList) != 0 {
		t.Fatalf("expected 0 VMs on hv-99, got %d", len(emptyList))
	}
}

func TestVMStoreListByHypervisorSorted(t *testing.T) {
	b := memory.New()
	store := b.VMs()
	ctx := context.Background()

	for _, name := range []string{"vm-c", "vm-a", "vm-b"} {
		v := vm.VirtualMachine{
			Name:          name,
			HypervisorRef: "hv-01",
		}
		if err := store.Upsert(ctx, v); err != nil {
			t.Fatalf("Upsert %s: %v", name, err)
		}
	}

	list, err := store.ListByHypervisor(ctx, "hv-01")
	if err != nil {
		t.Fatalf("ListByHypervisor: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	if list[0].Name != "vm-a" {
		t.Fatalf("expected first vm-a, got %s", list[0].Name)
	}
	if list[1].Name != "vm-b" {
		t.Fatalf("expected second vm-b, got %s", list[1].Name)
	}
	if list[2].Name != "vm-c" {
		t.Fatalf("expected third vm-c, got %s", list[2].Name)
	}
}

func TestVMStoreDeleteNonExistent(t *testing.T) {
	b := memory.New()
	store := b.VMs()
	ctx := context.Background()

	err := store.Delete(ctx, "ghost")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- CloudInitStore ---
