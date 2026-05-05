package discovery_test

import (
	"context"
	"testing"

	"github.com/sugaf1204/gomi/internal/discovery"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
)

func TestHandlePXEBoot_ExistingMAC(t *testing.T) {
	b := memory.New()
	store := b.Machines()
	svc := discovery.NewService(store)
	ctx := context.Background()

	existing := machine.Machine{
		Name:     "existing-01",
		Hostname: "existing-01.lab",
		MAC:      "aa:bb:cc:dd:ee:01",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseReady,
	}
	if err := store.Upsert(ctx, existing); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := svc.HandlePXEBoot(ctx, "aa:bb:cc:dd:ee:01", "", "amd64", "uefi")
	if err != nil {
		t.Fatalf("HandlePXEBoot: %v", err)
	}
	if got.Name != "existing-01" {
		t.Fatalf("expected existing machine name existing-01, got %s", got.Name)
	}
}

func TestHandlePXEBoot_UnknownMAC(t *testing.T) {
	b := memory.New()
	store := b.Machines()
	svc := discovery.NewService(store)
	ctx := context.Background()

	got, err := svc.HandlePXEBoot(ctx, "ff:ff:ff:ff:ff:01", "new-host", "amd64", "uefi")
	if err != nil {
		t.Fatalf("HandlePXEBoot: %v", err)
	}
	if got.Phase != machine.PhaseDiscovered {
		t.Fatalf("expected phase Discovered, got %s", got.Phase)
	}
	if got.Name != "new-host" {
		t.Fatalf("expected name new-host, got %s", got.Name)
	}
}
