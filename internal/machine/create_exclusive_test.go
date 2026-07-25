package machine_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

func newExclusiveTestMachine(name string) machine.Machine {
	return machine.Machine{
		Name:     name,
		Hostname: name,
		MAC:      "52:54:00:00:00:01",
		Arch:     "x86_64",
		Firmware: machine.FirmwareUEFI,
		Power:    power.PowerConfig{Type: power.PowerTypeManual},
		OSPreset: machine.OSPreset{Family: machine.OSTypeUbuntu, Version: "24.04", ImageRef: "img"},
	}
}

func TestMachineCreateExclusiveRejectsDuplicateName(t *testing.T) {
	svc := machine.NewService(memory.New().Machines())
	ctx := context.Background()

	if _, err := svc.CreateExclusive(ctx, newExclusiveTestMachine("m-once")); err != nil {
		t.Fatalf("first CreateExclusive: %v", err)
	}
	if _, err := svc.CreateExclusive(ctx, newExclusiveTestMachine("m-once")); !errors.Is(err, resource.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

// Two Quick Deploy clicks from separate tabs share the saved counter, so the
// UI reservation cannot coordinate them; only the store can.
func TestMachineCreateExclusiveIsAtomicUnderConcurrency(t *testing.T) {
	svc := machine.NewService(memory.New().Machines())
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
			_, err := svc.CreateExclusive(ctx, newExclusiveTestMachine("m-race"))
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
		t.Fatalf("expected exactly one machine record, got %d", len(items))
	}
}

func TestMachineCreateStillOverwrites(t *testing.T) {
	svc := machine.NewService(memory.New().Machines())
	ctx := context.Background()

	if _, err := svc.Create(ctx, newExclusiveTestMachine("m-upsert")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := svc.Create(ctx, newExclusiveTestMachine("m-upsert")); err != nil {
		t.Fatalf("second Create should still upsert, got %v", err)
	}
}
