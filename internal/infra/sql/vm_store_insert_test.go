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

// Domain and SubnetRef feed the embedded/rfc2136 DNS controllers' zone
// resolution (see internal/infra/dns/embedded.go). A field missing from the
// spec JSON round-trip silently drops the VM from DNS sync.
func TestVMStoreUpsertRoundTripsDomain(t *testing.T) {
	s := newTestBackend(t).VMs()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	v := vm.VirtualMachine{
		Name:          "vm-domain-roundtrip",
		HypervisorRef: "hv1",
		Resources:     vm.ResourceSpec{CPUCores: 1},
		SubnetRef:     "default",
		Domain:        "canvm.jp",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.Insert(ctx, v); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := s.Get(ctx, v.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Domain != "canvm.jp" {
		t.Fatalf("expected domain to round-trip, got %q", got.Domain)
	}
	if got.SubnetRef != "default" {
		t.Fatalf("expected subnetRef to round-trip, got %q", got.SubnetRef)
	}
}

// The token comparison must be part of the DELETE. A separate lookup followed
// by a delete-by-name would remove a replacement that took the name in between.
func TestVMStoreDeleteCreatedTokenMatchesGeneration(t *testing.T) {
	s := newTestBackend(t).VMs()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	v := vm.VirtualMachine{
		Name:          "vm-generation",
		HypervisorRef: "hv1",
		Resources:     vm.ResourceSpec{CPUCores: 1},
		Provisioning:  vm.ProvisioningStatus{Active: true, CompletionToken: "token-replacement"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.Insert(ctx, v); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// A stale rollback carrying the previous generation's token must not match.
	deleted, err := s.DeleteCreatedToken(ctx, v.Name, "token-original")
	if err != nil {
		t.Fatalf("DeleteCreatedToken with stale token: %v", err)
	}
	if deleted {
		t.Fatal("stale rollback deleted the replacement row")
	}
	if _, err := s.Get(ctx, v.Name); err != nil {
		t.Fatalf("replacement must survive: %v", err)
	}

	deleted, err = s.DeleteCreatedToken(ctx, v.Name, "token-replacement")
	if err != nil {
		t.Fatalf("DeleteCreatedToken with owning token: %v", err)
	}
	if !deleted {
		t.Fatal("owning generation should have been deleted")
	}
	if _, err := s.Get(ctx, v.Name); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected the row to be gone, got %v", err)
	}
}
