package api

import (
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/libvirt"
	"github.com/sugaf1204/gomi/internal/vm"
)

func TestMapVMPhaseFromDomainState_ProvisioningActiveKeepsProvisioning(t *testing.T) {
	got := vm.MapVMPhaseFromDomainState(libvirt.StateRunning, vm.PhaseProvisioning, vm.ProvisioningStatus{
		Active: true,
	})
	if got != vm.PhaseProvisioning {
		t.Fatalf("expected provisioning phase, got %s", got)
	}
}

func TestMapVMPhaseFromDomainState_ProvisioningCompleteAllowsRunning(t *testing.T) {
	now := time.Now().UTC()
	got := vm.MapVMPhaseFromDomainState(libvirt.StateRunning, vm.PhaseProvisioning, vm.ProvisioningStatus{
		Active:      false,
		CompletedAt: &now,
	})
	if got != vm.PhaseRunning {
		t.Fatalf("expected running phase, got %s", got)
	}
}

func TestIsProvisioningTimedOut(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	if !vm.IsProvisioningTimedOut(vm.ProvisioningStatus{
		Active:     true,
		DeadlineAt: &past,
	}, now) {
		t.Fatal("expected provisioning timeout")
	}
}
