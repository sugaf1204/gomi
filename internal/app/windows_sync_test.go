package app

import (
	"context"
	"github.com/sugaf1204/gomi/internal/infra/config"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"testing"
	"time"
)

func TestWindowsSyncSkipsLinuxArtifacts(t *testing.T) {
	ctx := context.Background()
	b := memory.New()
	now := time.Now().UTC()
	deadline := now.Add(time.Hour)
	m := machine.Machine{Name: "windows", OSPreset: machine.OSPreset{Family: machine.OSTypeWindows}, Phase: machine.PhaseProvisioning, Provision: &machine.ProvisionProgress{Active: true, AttemptID: "attempt", CompletionToken: "token", DeadlineAt: &deadline, Artifacts: map[string]string{"windowsInstallStarted": "true"}}}
	if err := b.Machines().Upsert(ctx, m); err != nil {
		t.Fatal(err)
	}
	r := Runtime{machineStore: b.Machines()} // No SSH store: Linux rendering must not run.
	r.syncProvisioningStates(ctx)
	got, err := b.Machines().Get(ctx, m.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Provision.Active || got.Provision.FinishedAt != nil || len(got.Provision.Artifacts) != 1 || got.Provision.Message != "" {
		t.Fatalf("Windows progress modified by Linux reconciler: %+v", got.Provision)
	}
}

func TestWindowsTimeoutCopiesProgressAndPreservesBoundary(t *testing.T) {
	ctx := context.Background()
	b := memory.New()
	now := time.Now().UTC()
	deadline := now
	m := machine.Machine{Name: "windows", OSPreset: machine.OSPreset{Family: machine.OSTypeWindows}, Phase: machine.PhaseProvisioning, Provision: &machine.ProvisionProgress{Active: true, AttemptID: "attempt", CompletionToken: "token", DeadlineAt: &deadline}}
	if err := b.Machines().Upsert(ctx, m); err != nil {
		t.Fatal(err)
	}
	r := Runtime{machineStore: b.Machines()}
	r.syncWindowsProvisioningState(ctx, m, now)
	got, _ := b.Machines().Get(ctx, m.Name)
	if !got.Provision.Active {
		t.Fatal("exact deadline boundary expired")
	}
	r.syncWindowsProvisioningState(ctx, m, now.Add(time.Second))
	got, _ = b.Machines().Get(ctx, m.Name)
	if got.Provision.Active || got.Phase != machine.PhaseError || got.LastError != "provisioning timed out" {
		t.Fatalf("timeout not applied: %+v", got)
	}
	if !m.Provision.Active {
		t.Fatal("snapshot/store-owned progress pointer mutated")
	}
}

type windowsSyncRaceStore struct {
	machine.Store
	beforeCAS func()
}

func (s *windowsSyncRaceStore) UpdateProvisioningAttempt(ctx context.Context, before, after machine.Machine) (bool, error) {
	s.beforeCAS()
	return s.Store.(machine.ProvisioningAttemptUpdater).UpdateProvisioningAttempt(ctx, before, after)
}

func TestWindowsTimeoutCannotOverwriteConcurrentCompletionOrReinstall(t *testing.T) {
	for _, mode := range []string{"complete", "reinstall"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			b := memory.New()
			now := time.Now().UTC()
			deadline := now.Add(-time.Minute)
			m := machine.Machine{Name: "windows", OSPreset: machine.OSPreset{Family: machine.OSTypeWindows}, Phase: machine.PhaseProvisioning, Provision: &machine.ProvisionProgress{Active: true, AttemptID: "attempt", CompletionToken: "token", DeadlineAt: &deadline}}
			if err := b.Machines().Upsert(ctx, m); err != nil {
				t.Fatal(err)
			}
			raced := m
			progress := *m.Provision
			raced.Provision = &progress
			if mode == "complete" {
				raced.Provision.Active = false
				raced.Provision.CompletedAt = &now
				raced.Phase = machine.PhaseReady
			} else {
				raced.Provision.AttemptID = "new-attempt"
				raced.Provision.CompletionToken = "new-token"
			}
			store := &windowsSyncRaceStore{Store: b.Machines(), beforeCAS: func() {
				if err := b.Machines().Upsert(ctx, raced); err != nil {
					t.Fatal(err)
				}
			}}
			r := Runtime{machineStore: store}
			r.syncProvisioningStates(ctx)
			got, _ := b.Machines().Get(ctx, m.Name)
			if got.Phase == machine.PhaseError || got.LastError != "" || got.Provision.AttemptID != raced.Provision.AttemptID || got.Provision.Active != raced.Provision.Active {
				t.Fatalf("timeout overwrote concurrent %s: %+v", mode, got)
			}
		})
	}
}

func TestLinuxProvisioningSyncStillBuildsArtifacts(t *testing.T) {
	for _, family := range []machine.OSType{machine.OSTypeUbuntu, "fedora"} {
		t.Run(string(family), func(t *testing.T) {
			ctx := context.Background()
			b := memory.New()
			m := machine.Machine{Name: "linux", Hostname: "linux", Firmware: machine.FirmwareUEFI, OSPreset: machine.OSPreset{Family: family}, Phase: machine.PhaseProvisioning, Provision: &machine.ProvisionProgress{Active: true}}
			if err := b.Machines().Upsert(ctx, m); err != nil {
				t.Fatal(err)
			}
			r := Runtime{machineStore: b.Machines(), sshkeyStore: b.SSHKeys(), Config: config.Config{BootHTTPBaseURL: "http://gomi/pxe"}}
			r.syncProvisioningStates(ctx)
			got, _ := b.Machines().Get(ctx, m.Name)
			if got.Provision.FinishedAt == nil || got.Provision.Artifacts["kernel"] == "" || got.Provision.Artifacts["installConfig.inline"] == "" || !got.Provision.Active {
				t.Fatalf("Linux artifacts changed: %+v", got.Provision)
			}
		})
	}
}
