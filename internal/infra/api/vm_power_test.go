package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/libvirt"
)

type fakeVMPowerOffExecutor struct {
	shutdownErr error
	destroyErr  error
	states      []libvirt.DomainState

	shutdownCalls int
	destroyCalls  int
	infoCalls     int
}

func (f *fakeVMPowerOffExecutor) ShutdownDomain(_ context.Context, _ string) error {
	f.shutdownCalls++
	return f.shutdownErr
}

func (f *fakeVMPowerOffExecutor) DestroyDomain(_ context.Context, _ string) error {
	f.destroyCalls++
	return f.destroyErr
}

func (f *fakeVMPowerOffExecutor) DomainInfo(_ context.Context, _ string) (*libvirt.DomainInfo, error) {
	f.infoCalls++
	if len(f.states) == 0 {
		return &libvirt.DomainInfo{State: libvirt.StateRunning}, nil
	}
	idx := f.infoCalls - 1
	if idx >= len(f.states) {
		idx = len(f.states) - 1
	}
	return &libvirt.DomainInfo{State: f.states[idx]}, nil
}

func TestPowerOffDomain_GracefulShutdown(t *testing.T) {
	originalTimeout := vmGracefulPowerOffTimeout
	originalPoll := vmPowerOffPollInterval
	vmGracefulPowerOffTimeout = 80 * time.Millisecond
	vmPowerOffPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		vmGracefulPowerOffTimeout = originalTimeout
		vmPowerOffPollInterval = originalPoll
	})

	exec := &fakeVMPowerOffExecutor{
		states: []libvirt.DomainState{
			libvirt.StateRunning,
			libvirt.StateShutoff,
		},
	}

	if err := powerOffDomain(context.Background(), exec, "vm-01"); err != nil {
		t.Fatalf("powerOffDomain returned error: %v", err)
	}
	if exec.shutdownCalls != 1 {
		t.Fatalf("expected shutdown call once, got %d", exec.shutdownCalls)
	}
	if exec.destroyCalls != 0 {
		t.Fatalf("expected no destroy fallback for graceful shutdown, got %d", exec.destroyCalls)
	}
}

func TestPowerOffDomain_FallsBackToDestroyOnTimeout(t *testing.T) {
	originalTimeout := vmGracefulPowerOffTimeout
	originalPoll := vmPowerOffPollInterval
	vmGracefulPowerOffTimeout = 40 * time.Millisecond
	vmPowerOffPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		vmGracefulPowerOffTimeout = originalTimeout
		vmPowerOffPollInterval = originalPoll
	})

	exec := &fakeVMPowerOffExecutor{
		states: []libvirt.DomainState{libvirt.StateRunning},
	}

	if err := powerOffDomain(context.Background(), exec, "vm-02"); err != nil {
		t.Fatalf("powerOffDomain returned error: %v", err)
	}
	if exec.destroyCalls != 1 {
		t.Fatalf("expected destroy fallback once, got %d", exec.destroyCalls)
	}
}

func TestPowerOffDomain_ReturnsShutdownError(t *testing.T) {
	exec := &fakeVMPowerOffExecutor{
		shutdownErr: errors.New("shutdown failed"),
	}
	if err := powerOffDomain(context.Background(), exec, "vm-03"); err == nil {
		t.Fatal("expected shutdown error")
	}
	if exec.destroyCalls != 0 {
		t.Fatalf("expected no destroy call when shutdown fails immediately, got %d", exec.destroyCalls)
	}
}

func TestPowerOffDomain_ReturnsDestroyErrorWhenFallbackFails(t *testing.T) {
	originalTimeout := vmGracefulPowerOffTimeout
	originalPoll := vmPowerOffPollInterval
	vmGracefulPowerOffTimeout = 40 * time.Millisecond
	vmPowerOffPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		vmGracefulPowerOffTimeout = originalTimeout
		vmPowerOffPollInterval = originalPoll
	})

	exec := &fakeVMPowerOffExecutor{
		states:     []libvirt.DomainState{libvirt.StateRunning},
		destroyErr: errors.New("destroy failed"),
	}

	err := powerOffDomain(context.Background(), exec, "vm-04")
	if err == nil {
		t.Fatal("expected fallback destroy error")
	}
	if !strings.Contains(err.Error(), "force power-off failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
