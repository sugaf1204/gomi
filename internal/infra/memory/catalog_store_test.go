package memory_test

import (
	"context"
	"errors"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
	"testing"
)

func TestCloudInitStoreCRUD(t *testing.T) {
	b := memory.New()
	store := b.CloudInits()
	ctx := context.Background()

	tpl := cloudinit.CloudInitTemplate{
		Name:     "tpl-01",
		UserData: "#cloud-config\n",
	}
	if err := store.Upsert(ctx, tpl); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get(ctx, "tpl-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserData != "#cloud-config\n" {
		t.Fatalf("unexpected userData: %s", got.UserData)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	if err := store.Delete(ctx, "tpl-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "tpl-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCloudInitStoreDeleteNonExistent(t *testing.T) {
	b := memory.New()
	store := b.CloudInits()
	ctx := context.Background()

	err := store.Delete(ctx, "ghost")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- OSImageStore ---

func TestOSImageStoreCRUD(t *testing.T) {
	b := memory.New()
	store := b.OSImages()
	ctx := context.Background()

	img := osimage.OSImage{
		Name:      "img-01",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
	}
	if err := store.Upsert(ctx, img); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get(ctx, "img-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.OSFamily != "ubuntu" {
		t.Fatalf("expected ubuntu, got %s", got.OSFamily)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	if err := store.Delete(ctx, "img-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "img-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestOSImageStoreDeleteNonExistent(t *testing.T) {
	b := memory.New()
	store := b.OSImages()
	ctx := context.Background()

	err := store.Delete(ctx, "ghost")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- Upsert overwrite ---

func TestStoreUpsertOverwrites(t *testing.T) {
	b := memory.New()
	store := b.VMs()
	ctx := context.Background()

	v1 := vm.VirtualMachine{
		Name:          "vm-01",
		HypervisorRef: "hv-01",
		Phase:         vm.PhasePending,
	}
	if err := store.Upsert(ctx, v1); err != nil {
		t.Fatalf("Upsert v1: %v", err)
	}

	v2 := v1
	v2.Phase = vm.PhaseRunning
	v2.HypervisorRef = "hv-02"
	if err := store.Upsert(ctx, v2); err != nil {
		t.Fatalf("Upsert v2: %v", err)
	}

	got, err := store.Get(ctx, "vm-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Phase != vm.PhaseRunning {
		t.Fatalf("expected phase Running, got %s", got.Phase)
	}
	if got.HypervisorRef != "hv-02" {
		t.Fatalf("expected hypervisorRef hv-02, got %s", got.HypervisorRef)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 VM after upsert, got %d", len(list))
	}
}
