package sql_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

// Exercises the real driver's duplicate-key error, which the in-memory store
// cannot cover: a translation helper that misses this driver would leave
// duplicate creates returning a generic 500 instead of 409.
func TestVMStoreInsertRejectsDuplicateName(t *testing.T) {
	s := newTestBackend(t).VMs()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	v := vm.VirtualMachine{
		Name:          "vm-insert-once",
		HypervisorRef: "hv1",
		Resources:     vm.ResourceSpec{CPUCores: 1},
		Phase:         vm.PhasePending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.Insert(ctx, v); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if err := s.Insert(ctx, v); !errors.Is(err, resource.ErrAlreadyExists) {
		t.Fatalf("expected resource.ErrAlreadyExists from the driver's unique violation, got %v", err)
	}

	// The losing insert must not have modified the stored record.
	got, err := s.Get(ctx, v.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.HypervisorRef != "hv1" {
		t.Fatalf("stored record was modified by the rejected insert: %+v", got)
	}
}

func TestVMStoreInsertThenUpsertStillOverwrites(t *testing.T) {
	s := newTestBackend(t).VMs()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	v := vm.VirtualMachine{
		Name:          "vm-insert-upsert",
		HypervisorRef: "hv1",
		Resources:     vm.ResourceSpec{CPUCores: 1},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.Insert(ctx, v); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	v.HypervisorRef = "hv2"
	if err := s.Upsert(ctx, v); err != nil {
		t.Fatalf("Upsert after Insert: %v", err)
	}
	got, err := s.Get(ctx, v.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.HypervisorRef != "hv2" {
		t.Fatalf("expected Upsert to overwrite, got hypervisorRef=%s", got.HypervisorRef)
	}
}
