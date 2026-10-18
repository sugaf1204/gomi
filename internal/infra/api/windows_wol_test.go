package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
)

func windowsWakeFixture(t *testing.T) (*Server, machine.Machine, *redeployPowerCycleExecutor) {
	t.Helper()
	s, m, e := newRedeployPowerCycleTestServer(t)
	m.OSPreset.Family = machine.OSType("windows")
	m.IPAssignment = machine.IPAssignmentModeDHCP
	m.Power = power.PowerConfig{Type: power.PowerTypeWoL, WoL: &power.WoLConfig{WakeMAC: m.MAC}}
	m.Provision = &machine.ProvisionProgress{Active: true, AttemptID: "windows-attempt"}
	mustUpsertMachine(t, s.machines, m)
	return s, m, e
}

func TestWindowsWakeProgressRequiresCurrentTypedAttempt(t *testing.T) {
	_, m, _ := windowsWakeFixture(t)
	m.Provision.Artifacts = map[string]string{"windowsInstallStarted": "true"}
	for _, tc := range []struct {
		name    string
		change  func(*machine.Machine)
		ok      bool
		wantErr bool
	}{
		{name: "authenticated Windows callback", ok: true},
		{name: "old attempt", change: func(v *machine.Machine) { p := *v.Provision; p.AttemptID = "old"; v.Provision = &p }, wantErr: true},
		{name: "Linux", change: func(v *machine.Machine) { v.OSPreset.Family = machine.OSTypeUbuntu }, wantErr: true},
		{name: "other MAC", change: func(v *machine.Machine) { v.MAC = "52:54:00:00:00:ff" }, wantErr: true},
		{name: "no callback", change: func(v *machine.Machine) { p := *v.Provision; p.Artifacts = nil; v.Provision = &p }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := m
			if tc.change != nil {
				tc.change(&v)
			}
			ok, err := windowsWakeProgress(v, m)
			if ok != tc.ok || (err != nil) != tc.wantErr {
				t.Fatalf("got %v %v", ok, err)
			}
		})
	}
}

func TestWindowsWakeUsesCallbackDespiteUnreachableOldIP(t *testing.T) {
	s, m, e := windowsWakeFixture(t)
	withRedeployPowerCycleTiming(t, time.Second, 100*time.Millisecond, 5*time.Millisecond, 0)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.powerOnWindowsRedeployMachine(ctx, machinePowerInfo(m, "192.0.2.103"), m) }()
	if action := waitRedeployPowerAction(t, e.calls); action != power.ActionPowerOn {
		t.Fatal(action)
	}
	current, _ := s.machines.Get(ctx, m.Name)
	p := *current.Provision
	p.Artifacts = map[string]string{"windowsInstallStarted": "true"}
	current.Provision = &p
	current.IP = "192.0.2.243"
	mustUpsertMachine(t, s.machines, current)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("callback did not confirm wake-up")
	}
}

func TestWindowsWakeRefreshesDHCPStatusAddress(t *testing.T) {
	s, m, e := windowsWakeFixture(t)
	current := m
	current.IP = "192.0.2.243"
	mustUpsertMachine(t, s.machines, current)
	e.setStatus(power.PowerStateRunning)
	ok, err := s.waitWindowsWake(context.Background(), machinePowerInfo(m, "192.0.2.103"), m)
	if err != nil || !ok {
		t.Fatalf("got %v %v", ok, err)
	}
	if info := waitRedeployPowerStatusInfo(t, e.statusInfos); info.IP != "192.0.2.243" {
		t.Fatalf("stale status address %s", info.IP)
	}
}

func TestWindowsPowerResultCannotOverrideProgressOrNewAttempt(t *testing.T) {
	for _, mode := range []string{"progress", "completed", "new attempt", "installer error", "success preserves error", "genuine wake failure"} {
		t.Run(mode, func(t *testing.T) {
			s, m, _ := windowsWakeFixture(t)
			current := m
			p := *m.Provision
			current.Provision = &p
			errorText := "old IP timeout"
			lastError := &errorText
			switch mode {
			case "progress":
				p.Artifacts = map[string]string{"windowsInstallStarted": "true"}
			case "completed":
				p.Active = false
				p.CompletedAt = &current.UpdatedAt
				p.CompletionSource = "windows"
				current.Phase = machine.PhaseReady
			case "new attempt":
				p.AttemptID = "replacement"
			case "installer error":
				current.LastError = "DISM failed"
			case "success preserves error":
				current.LastError = "DISM failed"
				lastError = nil
			}
			mustUpsertMachine(t, s.machines, current)
			s.recordWindowsRedeployPowerAction(m, power.ActionPowerOn, lastError)
			got, _ := s.machines.Get(context.Background(), m.Name)
			if mode == "genuine wake failure" {
				if !strings.Contains(got.LastError, "old IP timeout") {
					t.Fatal(got.LastError)
				}
			} else if got.LastError != current.LastError {
				t.Fatalf("installer/new attempt error changed: %q", got.LastError)
			}
			if got.Provision.AttemptID != p.AttemptID {
				t.Fatal("attempt overwritten")
			}
		})
	}
}

type windowsPowerInterleavingStore struct {
	machine.Store
	updater   machine.ProvisioningAttemptUpdater
	beforeCAS func()
}

func (s *windowsPowerInterleavingStore) UpdateProvisioningAttempt(ctx context.Context, before, after machine.Machine) (bool, error) {
	if s.beforeCAS != nil {
		f := s.beforeCAS
		s.beforeCAS = nil
		f()
	}
	return s.updater.UpdateProvisioningAttempt(ctx, before, after)
}

func TestWindowsPowerTimeoutCASRejectsConcurrentNewAttempt(t *testing.T) {
	s, m, _ := windowsWakeFixture(t)
	original := s.machines.Store()
	interleaved := &windowsPowerInterleavingStore{Store: original, updater: original.(machine.ProvisioningAttemptUpdater)}
	interleaved.beforeCAS = func() {
		current, _ := original.Get(context.Background(), m.Name)
		p := *current.Provision
		p.AttemptID = "replacement"
		current.Provision = &p
		current.LastError = "new attempt failure"
		if err := original.Upsert(context.Background(), current); err != nil {
			t.Fatal(err)
		}
	}
	s.machines = machine.NewService(interleaved)
	failure := "old power timeout"
	s.recordWindowsRedeployPowerAction(m, power.ActionPowerOn, &failure)
	got, _ := original.Get(context.Background(), m.Name)
	if got.Provision.AttemptID != "replacement" || got.LastError != "new attempt failure" {
		t.Fatal("late result modified replacement attempt")
	}
}

func TestWindowsWoLDoesNotChangeOtherPowerPaths(t *testing.T) {
	for _, family := range []machine.OSType{machine.OSTypeUbuntu, machine.OSType("fedora"), machine.OSType("windows")} {
		for _, kind := range []power.PowerType{power.PowerTypeIPMI, power.PowerTypeWebhook} {
			t.Run(string(family)+"/"+string(kind), func(t *testing.T) {
				s, m, e := newRedeployPowerCycleTestServer(t)
				m.OSPreset.Family = family
				m.Power.Type = kind
				mustUpsertMachine(t, s.machines, m)
				withRedeployPowerCycleTiming(t, 500*time.Millisecond, 100*time.Millisecond, 5*time.Millisecond, 0)
				s.startRedeployPowerCycle(m, m, "")
				if got := waitRedeployPowerAction(t, e.calls); got != power.ActionPowerOff {
					t.Fatal(got)
				}
				if got := waitRedeployPowerAction(t, e.calls); got != power.ActionPowerOn {
					t.Fatal(got)
				}
				waitMachinePowerOnConfirmed(t, s.machines, m.Name)
			})
		}
	}
}

func TestWindowsPowerCASPreservesConcurrentInstallerError(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(map[bool]string{false: "late timeout", true: "success"}[success], func(t *testing.T) {
			s, m, _ := windowsWakeFixture(t)
			original := s.machines.Store()
			interleaved := &windowsPowerInterleavingStore{Store: original, updater: original.(machine.ProvisioningAttemptUpdater)}
			interleaved.beforeCAS = func() {
				current, _ := original.Get(context.Background(), m.Name)
				current.LastError = "concurrent installer failure"
				if err := original.Upsert(context.Background(), current); err != nil {
					t.Fatal(err)
				}
			}
			s.machines = machine.NewService(interleaved)
			failure := "power timeout"
			lastError := &failure
			if success {
				lastError = nil
			}
			s.recordWindowsRedeployPowerAction(m, power.ActionPowerOn, lastError)
			got, _ := original.Get(context.Background(), m.Name)
			if got.LastError != "concurrent installer failure" {
				t.Fatalf("CAS lost installer error: %q", got.LastError)
			}
		})
	}
}
