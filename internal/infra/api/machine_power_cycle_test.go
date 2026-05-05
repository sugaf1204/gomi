package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
)

type redeployPowerCycleExecutor struct {
	calls       chan power.Action
	infos       chan power.MachineInfo
	statusInfos chan power.MachineInfo
	executeErr  map[power.Action]error
	statusState power.PowerState
}

func newRedeployPowerCycleExecutor() *redeployPowerCycleExecutor {
	return &redeployPowerCycleExecutor{
		calls:       make(chan power.Action, 64),
		infos:       make(chan power.MachineInfo, 64),
		statusInfos: make(chan power.MachineInfo, 64),
		executeErr:  make(map[power.Action]error),
		statusState: power.PowerStateStopped,
	}
}

func (e *redeployPowerCycleExecutor) Execute(_ context.Context, mi power.MachineInfo, action power.Action) error {
	e.calls <- action
	e.infos <- mi
	if err := e.executeErr[action]; err != nil {
		return err
	}
	return nil
}

func (e *redeployPowerCycleExecutor) CheckStatus(_ context.Context, mi power.MachineInfo) (power.PowerState, error) {
	e.statusInfos <- mi
	return e.statusState, nil
}

func (e *redeployPowerCycleExecutor) ConfigureBootOrder(_ context.Context, _ power.MachineInfo, _ power.BootOrder) error {
	return nil
}

func TestStartRedeployPowerCycle_SkipsManualPower(t *testing.T) {
	srv, m, exec := newRedeployPowerCycleTestServer(t)
	m.Power = power.PowerConfig{Type: power.PowerTypeManual}

	srv.startRedeployPowerCycle(m, m, "")

	assertNoRedeployPowerAction(t, exec.calls)
}

func TestStartRedeployPowerCycle_StopsAfterPowerOffFailure(t *testing.T) {
	srv, m, exec := newRedeployPowerCycleTestServer(t)
	exec.executeErr[power.ActionPowerOff] = errors.New("shutdown failed")

	srv.startRedeployPowerCycle(m, m, "")

	if got := waitRedeployPowerAction(t, exec.calls); got != power.ActionPowerOff {
		t.Fatalf("expected first power action power-off, got %s", got)
	}
	assertNoRedeployPowerAction(t, exec.calls)
	got := waitMachineLastError(t, srv.machines, m.Name)
	if !strings.Contains(got, "shutdown failed") {
		t.Fatalf("expected shutdown failure in LastError, got %q", got)
	}
}

func TestStartRedeployPowerCycle_StopsWhenPowerOffStateIsNotConfirmed(t *testing.T) {
	srv, m, exec := newRedeployPowerCycleTestServer(t)
	exec.statusState = power.PowerStateRunning

	withRedeployPowerCycleTiming(t, 200*time.Millisecond, 30*time.Millisecond, 5*time.Millisecond, 0)

	srv.startRedeployPowerCycle(m, m, "")

	if got := waitRedeployPowerAction(t, exec.calls); got != power.ActionPowerOff {
		t.Fatalf("expected first power action power-off, got %s", got)
	}
	assertNoRedeployPowerAction(t, exec.calls)
	got := waitMachineLastError(t, srv.machines, m.Name)
	if !strings.Contains(got, "did not report stopped") {
		t.Fatalf("expected stopped timeout in LastError, got %q", got)
	}
}

func TestStartRedeployPowerCycle_WaitsBeforeWoLPowerOn(t *testing.T) {
	srv, m, exec := newRedeployPowerCycleTestServer(t)
	m.Power = power.PowerConfig{
		Type: power.PowerTypeWoL,
		WoL: &power.WoLConfig{
			WakeMAC:        m.MAC,
			BroadcastIP:    "255.255.255.255",
			Port:           9,
			ShutdownTarget: m.IP,
			HMACSecret:     "secret",
			Token:          "token",
		},
	}
	mustUpsertMachine(t, srv.machines, m)

	minSettle := 40 * time.Millisecond
	withRedeployPowerCycleTiming(t, 300*time.Millisecond, 120*time.Millisecond, 5*time.Millisecond, minSettle)

	started := time.Now()
	srv.startRedeployPowerCycle(m, m, "")

	if got := waitRedeployPowerAction(t, exec.calls); got != power.ActionPowerOff {
		t.Fatalf("expected first power action power-off, got %s", got)
	}
	if got := waitRedeployPowerAction(t, exec.calls); got != power.ActionPowerOn {
		t.Fatalf("expected second power action power-on, got %s", got)
	}
	if elapsed := time.Since(started); elapsed < minSettle {
		t.Fatalf("expected WoL power-on after at least %s, got %s", minSettle, elapsed)
	}
}

func TestStartRedeployPowerCycle_PowerOffUsesPreviousWoLCredentials(t *testing.T) {
	srv, before, exec := newRedeployPowerCycleTestServer(t)
	before.Power = power.PowerConfig{
		Type: power.PowerTypeWoL,
		WoL: &power.WoLConfig{
			WakeMAC:    before.MAC,
			HMACSecret: "old-secret",
			Token:      "old-token",
		},
	}
	after := before
	after.Power = power.PowerConfig{
		Type: power.PowerTypeWoL,
		WoL: &power.WoLConfig{
			WakeMAC:    before.MAC,
			HMACSecret: "new-secret",
			Token:      "new-token",
		},
	}
	mustUpsertMachine(t, srv.machines, after)

	withRedeployPowerCycleTiming(t, 300*time.Millisecond, 120*time.Millisecond, 5*time.Millisecond, 0)
	srv.startRedeployPowerCycle(before, after, "")

	if got := waitRedeployPowerAction(t, exec.calls); got != power.ActionPowerOff {
		t.Fatalf("expected first power action power-off, got %s", got)
	}
	info := waitRedeployPowerInfo(t, exec.infos)
	if info.Power.WoL == nil || info.Power.WoL.HMACSecret != "old-secret" || info.Power.WoL.Token != "old-token" {
		t.Fatalf("expected power-off to use old WoL credentials, got %+v", info.Power.WoL)
	}
	if got := waitRedeployPowerAction(t, exec.calls); got != power.ActionPowerOn {
		t.Fatalf("expected second power action power-on, got %s", got)
	}
	info = waitRedeployPowerInfo(t, exec.infos)
	if info.Power.WoL == nil || info.Power.WoL.HMACSecret != "new-secret" || info.Power.WoL.Token != "new-token" {
		t.Fatalf("expected power-on to use new WoL credentials, got %+v", info.Power.WoL)
	}
}

func TestStartRedeployPowerCycle_UsesShutdownTargetForWoLStatusWhenIPIsEmpty(t *testing.T) {
	srv, m, exec := newRedeployPowerCycleTestServer(t)
	m.IP = ""
	m.Power = power.PowerConfig{
		Type: power.PowerTypeWoL,
		WoL: &power.WoLConfig{
			WakeMAC:        m.MAC,
			BroadcastIP:    "255.255.255.255",
			Port:           9,
			ShutdownTarget: "node-test-shutdown.local",
			HMACSecret:     "secret",
			Token:          "token",
		},
	}
	mustUpsertMachine(t, srv.machines, m)

	withRedeployPowerCycleTiming(t, 300*time.Millisecond, 120*time.Millisecond, 5*time.Millisecond, 0)
	srv.startRedeployPowerCycle(m, m, "")

	if got := waitRedeployPowerAction(t, exec.calls); got != power.ActionPowerOff {
		t.Fatalf("expected first power action power-off, got %s", got)
	}
	info := waitRedeployPowerStatusInfo(t, exec.statusInfos)
	if info.IP != "node-test-shutdown.local" {
		t.Fatalf("expected status probe to use shutdownTarget, got %q", info.IP)
	}
}

func newRedeployPowerCycleTestServer(t *testing.T) (*Server, machine.Machine, *redeployPowerCycleExecutor) {
	t.Helper()
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	exec := newRedeployPowerCycleExecutor()
	srv := &Server{machines: machineSvc, powerExecutor: exec}
	m := machine.Machine{
		Name:         "node-test",
		Hostname:     "node-test",
		MAC:          "52:54:00:de:ad:20",
		IP:           "192.0.2.20",
		Arch:         "amd64",
		Firmware:     machine.FirmwareUEFI,
		Power:        power.PowerConfig{Type: power.PowerTypeWebhook, Webhook: &power.WebhookConfig{PowerOnURL: "https://power.example/on", PowerOffURL: "https://power.example/off"}},
		OSPreset:     machine.OSPreset{Family: machine.OSTypeUbuntu, Version: "24.04", ImageRef: "ubuntu-24.04"},
		IPAssignment: machine.IPAssignmentModeStatic,
		Phase:        machine.PhaseProvisioning,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	mustUpsertMachine(t, machineSvc, m)
	return srv, m, exec
}

func mustUpsertMachine(t *testing.T, svc *machine.Service, m machine.Machine) {
	t.Helper()
	if err := svc.Store().Upsert(context.Background(), m); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
}

func waitRedeployPowerAction(t *testing.T, calls <-chan power.Action) power.Action {
	t.Helper()
	select {
	case action := <-calls:
		return action
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for redeploy power action")
		return ""
	}
}

func waitRedeployPowerInfo(t *testing.T, infos <-chan power.MachineInfo) power.MachineInfo {
	t.Helper()
	select {
	case info := <-infos:
		return info
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for redeploy power info")
		return power.MachineInfo{}
	}
}

func waitRedeployPowerStatusInfo(t *testing.T, infos <-chan power.MachineInfo) power.MachineInfo {
	t.Helper()
	select {
	case info := <-infos:
		return info
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for redeploy power status info")
		return power.MachineInfo{}
	}
}

func assertNoRedeployPowerAction(t *testing.T, calls <-chan power.Action) {
	t.Helper()
	select {
	case action := <-calls:
		t.Fatalf("unexpected redeploy power action: %s", action)
	case <-time.After(75 * time.Millisecond):
	}
}

func waitMachineLastError(t *testing.T, svc *machine.Service, name string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		m, err := svc.Get(context.Background(), name)
		if err != nil {
			t.Fatalf("get machine: %v", err)
		}
		if m.LastError != "" {
			return m.LastError
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for LastError on %s", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func withRedeployPowerCycleTiming(t *testing.T, cycleTimeout, settleTimeout, pollInterval, wolMinimumSettle time.Duration) {
	t.Helper()
	origCycleTimeout := redeployPowerCycleTimeout
	origSettleTimeout := redeployPowerOffSettleTimeout
	origPollInterval := redeployPowerOffPollInterval
	origWoLMinimumSettle := redeployWoLPowerOffMinimumSettle
	redeployPowerCycleTimeout = cycleTimeout
	redeployPowerOffSettleTimeout = settleTimeout
	redeployPowerOffPollInterval = pollInterval
	redeployWoLPowerOffMinimumSettle = wolMinimumSettle
	t.Cleanup(func() {
		redeployPowerCycleTimeout = origCycleTimeout
		redeployPowerOffSettleTimeout = origSettleTimeout
		redeployPowerOffPollInterval = origPollInterval
		redeployWoLPowerOffMinimumSettle = origWoLMinimumSettle
	})
}
