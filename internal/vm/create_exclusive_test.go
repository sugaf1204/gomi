package vm_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

func newExclusiveTestVM(name string) vm.VirtualMachine {
	return vm.VirtualMachine{
		Name:          name,
		HypervisorRef: "hv-exclusive",
		Resources:     vm.ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
		OSImageRef:    "ubuntu-test",
	}
}

func TestCreateExclusiveRejectsDuplicateName(t *testing.T) {
	svc := vm.NewService(memory.New().VMs())
	ctx := context.Background()

	if _, err := svc.CreateExclusive(ctx, newExclusiveTestVM("vm-once")); err != nil {
		t.Fatalf("first CreateExclusive: %v", err)
	}
	if _, err := svc.CreateExclusive(ctx, newExclusiveTestVM("vm-once")); !errors.Is(err, resource.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

// A sequential test cannot distinguish an atomic insert from a check-then-write,
// which is the bug this guards against: two concurrent requests can both find
// the name free and both succeed, one silently overwriting the other.
func TestCreateExclusiveIsAtomicUnderConcurrency(t *testing.T) {
	svc := vm.NewService(memory.New().VMs())
	ctx := context.Background()

	const racers = 8
	start := make(chan struct{})
	results := make(chan error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.CreateExclusive(ctx, newExclusiveTestVM("vm-race"))
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	created, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			created++
		case errors.Is(err, resource.ErrAlreadyExists):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if created != 1 || conflicts != racers-1 {
		t.Fatalf("expected exactly 1 success and %d conflicts, got %d and %d", racers-1, created, conflicts)
	}

	items, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected exactly one VM record, got %d", len(items))
	}
}

// Create keeps its overwrite semantics; only CreateExclusive rejects duplicates.
func TestCreateStillOverwrites(t *testing.T) {
	svc := vm.NewService(memory.New().VMs())
	ctx := context.Background()

	if _, err := svc.Create(ctx, newExclusiveTestVM("vm-upsert")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := svc.Create(ctx, newExclusiveTestVM("vm-upsert")); err != nil {
		t.Fatalf("second Create should still upsert, got %v", err)
	}
}
