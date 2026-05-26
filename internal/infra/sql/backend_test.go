package sql_test

import (
	"context"
	"errors"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
	"testing"
	"time"
)

func newTestBackend(t *testing.T) *infrasql.Backend {
	t.Helper()
	b, err := infrasql.New("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestMachineStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.Machines()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	m := machine.Machine{
		Name:      "srv1",
		Hostname:  "srv1.local",
		MAC:       "AA:BB:CC:DD:EE:01",
		Arch:      "amd64",
		Firmware:  machine.FirmwareUEFI,
		Power:     power.PowerConfig{Type: power.PowerTypeManual},
		Phase:     machine.PhaseReady,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.Upsert(ctx, m); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.Get(ctx, "srv1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Hostname != "srv1.local" {
		t.Errorf("hostname = %q, want srv1.local", got.Hostname)
	}

	// GetByMAC (case-insensitive)
	got, err = s.GetByMAC(ctx, "aa:bb:cc:dd:ee:01")
	if err != nil {
		t.Fatalf("GetByMAC: %v", err)
	}
	if got.Name != "srv1" {
		t.Errorf("GetByMAC name = %q, want srv1", got.Name)
	}

	// List
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	// Delete
	if err := s.Delete(ctx, "srv1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = s.Get(ctx, "srv1")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("Get after delete: got %v, want ErrNotFound", err)
	}

	// Delete non-existent
	if err := s.Delete(ctx, "nope"); !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("Delete non-existent: got %v, want ErrNotFound", err)
	}
}

func TestMachineStorePartialMachineUpdatesPreserveUnrelatedFields(t *testing.T) {
	b := newTestBackend(t)
	s := b.Machines()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	stateAt := now.Add(1 * time.Minute)
	finishedAt := now.Add(2 * time.Minute)

	m := machine.Machine{
		Name:                     "srv-partial",
		Hostname:                 "srv-partial.local",
		MAC:                      "AA:BB:CC:DD:EE:02",
		IP:                       "192.0.2.10",
		Arch:                     "amd64",
		Firmware:                 machine.FirmwareUEFI,
		Power:                    power.PowerConfig{Type: power.PowerTypeManual},
		Phase:                    machine.PhaseProvisioning,
		Provision:                &machine.ProvisionProgress{Active: true, AttemptID: "attempt-partial", FinishedAt: &finishedAt},
		LastPowerAction:          "power-off",
		LastDeployedCloudInitRef: "cloudinit-a",
		LastError:                "old-error",
		PowerState:               power.PowerStateRunning,
		PowerStateAt:             &stateAt,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	if err := s.Upsert(ctx, m); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := s.UpdatePowerActionStatus(ctx, m.Name, power.ActionPowerOn, nil, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("UpdatePowerActionStatus nil error: %v", err)
	}
	got, err := s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after action update: %v", err)
	}
	if got.LastPowerAction != string(power.ActionPowerOn) {
		t.Fatalf("LastPowerAction = %q, want power-on", got.LastPowerAction)
	}
	if got.LastError != "old-error" {
		t.Fatalf("LastError changed on nil update: %q", got.LastError)
	}
	if got.Provision == nil || got.Provision.AttemptID != "attempt-partial" || !got.Provision.Active {
		t.Fatalf("Provision was not preserved: %#v", got.Provision)
	}
	if got.LastDeployedCloudInitRef != "cloudinit-a" {
		t.Fatalf("LastDeployedCloudInitRef = %q", got.LastDeployedCloudInitRef)
	}
	if got.PowerState != power.PowerStateRunning || got.PowerStateAt == nil || !got.PowerStateAt.Equal(stateAt) {
		t.Fatalf("power state changed unexpectedly: state=%q at=%v", got.PowerState, got.PowerStateAt)
	}

	nextErr := "next-error"
	if err := s.UpdatePowerActionStatus(ctx, m.Name, power.ActionPowerOff, &nextErr, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("UpdatePowerActionStatus error: %v", err)
	}
	got, err = s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after error update: %v", err)
	}
	if got.LastPowerAction != string(power.ActionPowerOff) || got.LastError != nextErr {
		t.Fatalf("power action fields not updated: action=%q error=%q", got.LastPowerAction, got.LastError)
	}

	nextStateAt := now.Add(5 * time.Minute)
	if err := s.UpdatePowerStateStatus(ctx, m.Name, power.PowerStateStopped, nextStateAt, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("UpdatePowerStateStatus: %v", err)
	}
	got, err = s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after state update: %v", err)
	}
	if got.PowerState != power.PowerStateStopped || got.PowerStateAt == nil || !got.PowerStateAt.Equal(nextStateAt) {
		t.Fatalf("power state fields not updated: state=%q at=%v", got.PowerState, got.PowerStateAt)
	}
	if got.LastPowerAction != string(power.ActionPowerOff) || got.LastError != nextErr {
		t.Fatalf("power action fields changed during state update: action=%q error=%q", got.LastPowerAction, got.LastError)
	}
	if got.Provision == nil || got.Provision.AttemptID != "attempt-partial" {
		t.Fatalf("Provision changed during state update: %#v", got.Provision)
	}

	if err := s.UpdateDynamicIPAddress(ctx, m.Name, m.MAC, "192.0.2.11", now.Add(6*time.Minute)); err != nil {
		t.Fatalf("UpdateDynamicIPAddress: %v", err)
	}
	got, err = s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after IP update: %v", err)
	}
	if got.IP != "192.0.2.11" {
		t.Fatalf("IP = %q, want 192.0.2.11", got.IP)
	}
	if got.LastPowerAction != string(power.ActionPowerOff) || got.LastError != nextErr || got.PowerState != power.PowerStateStopped {
		t.Fatalf("status changed during IP update: action=%q error=%q state=%q", got.LastPowerAction, got.LastError, got.PowerState)
	}

	if err := s.UpdateDynamicIPAddress(ctx, m.Name, "AA:BB:CC:DD:EE:FF", "192.0.2.12", now.Add(7*time.Minute)); err != nil {
		t.Fatalf("UpdateDynamicIPAddress mismatched MAC: %v", err)
	}
	got, err = s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after mismatched MAC update: %v", err)
	}
	if got.IP != "192.0.2.11" {
		t.Fatalf("IP changed despite MAC mismatch: %q", got.IP)
	}

	got.IPAssignment = machine.IPAssignmentModeStatic
	got.IP = "192.0.2.50"
	if err := s.Upsert(ctx, got); err != nil {
		t.Fatalf("Upsert static machine: %v", err)
	}
	if err := s.UpdateDynamicIPAddress(ctx, m.Name, m.MAC, "192.0.2.13", now.Add(8*time.Minute)); err != nil {
		t.Fatalf("UpdateDynamicIPAddress static machine: %v", err)
	}
	got, err = s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after static IP update: %v", err)
	}
	if got.IP != "192.0.2.50" {
		t.Fatalf("static IP changed during dynamic IP update: %q", got.IP)
	}

	if err := s.UpdatePowerActionStatus(ctx, "missing", power.ActionPowerOn, nil, now); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("UpdatePowerActionStatus missing = %v, want ErrNotFound", err)
	}
	if err := s.UpdatePowerStateStatus(ctx, "missing", power.PowerStateStopped, now, now); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("UpdatePowerStateStatus missing = %v, want ErrNotFound", err)
	}
	if err := s.UpdateDynamicIPAddress(ctx, "missing", m.MAC, "192.0.2.12", now); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("UpdateDynamicIPAddress missing = %v, want ErrNotFound", err)
	}
}
