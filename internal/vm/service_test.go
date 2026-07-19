package vm_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

func newTestService() *vm.Service {
	b := memory.New()
	return vm.NewService(b.VMs())
}

func testVM() vm.VirtualMachine {
	return vm.VirtualMachine{
		Name:          "vm-test-01",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		OSImageRef:    "ubuntu-24.04",
	}
}

func TestServiceCreate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	v, err := svc.Create(ctx, testVM())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if v.PowerControlMethod != vm.PowerControlLibvirt {
		t.Fatalf("expected powerControlMethod libvirt, got %s", v.PowerControlMethod)
	}
}

func TestServiceGetAndList(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testVM())

	got, err := svc.Get(ctx, "vm-test-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "vm-test-01" {
		t.Fatalf("expected name vm-test-01, got %s", got.Name)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 VM, got %d", len(list))
	}
}

func TestServiceListByHypervisor(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	vm1 := testVM()
	vm1.Name = "vm-a"
	vm1.HypervisorRef = "hv-01"
	_, _ = svc.Create(ctx, vm1)

	vm2 := testVM()
	vm2.Name = "vm-b"
	vm2.HypervisorRef = "hv-02"
	_, _ = svc.Create(ctx, vm2)

	list, err := svc.ListByHypervisor(ctx, "hv-01")
	if err != nil {
		t.Fatalf("ListByHypervisor: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 VM on hv-01, got %d", len(list))
	}
}

func TestServiceDelete(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testVM())

	if err := svc.Delete(ctx, "vm-test-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := svc.Get(ctx, "vm-test-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestServiceUpdateStatus(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testVM())

	updated, err := svc.UpdateStatus(ctx, "vm-test-01", vm.PhaseRunning, "power-on", "")
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if updated.Phase != vm.PhaseRunning {
		t.Fatalf("expected phase Running, got %s", updated.Phase)
	}
}

func TestServiceListByHypervisorMultiple(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	// Create 3 VMs on hv-01 and 2 on hv-02.
	for i, name := range []string{"vm-h1-a", "vm-h1-b", "vm-h1-c"} {
		v := testVM()
		v.Name = name
		v.HypervisorRef = "hv-01"
		v.Resources.CPUCores = i + 1
		if _, err := svc.Create(ctx, v); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	for _, name := range []string{"vm-h2-a", "vm-h2-b"} {
		v := testVM()
		v.Name = name
		v.HypervisorRef = "hv-02"
		if _, err := svc.Create(ctx, v); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	hv1List, err := svc.ListByHypervisor(ctx, "hv-01")
	if err != nil {
		t.Fatalf("ListByHypervisor hv-01: %v", err)
	}
	if len(hv1List) != 3 {
		t.Fatalf("expected 3 VMs on hv-01, got %d", len(hv1List))
	}

	hv2List, err := svc.ListByHypervisor(ctx, "hv-02")
	if err != nil {
		t.Fatalf("ListByHypervisor hv-02: %v", err)
	}
	if len(hv2List) != 2 {
		t.Fatalf("expected 2 VMs on hv-02, got %d", len(hv2List))
	}

	// Non-existent hypervisor should return empty list.
	hv3List, err := svc.ListByHypervisor(ctx, "hv-99")
	if err != nil {
		t.Fatalf("ListByHypervisor hv-99: %v", err)
	}
	if len(hv3List) != 0 {
		t.Fatalf("expected 0 VMs on hv-99, got %d", len(hv3List))
	}
}

func TestServiceUpdateStatusPhaseTransitions(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testVM())

	// Pending -> Creating
	updated, err := svc.UpdateStatus(ctx, "vm-test-01", vm.PhaseCreating, "define", "")
	if err != nil {
		t.Fatalf("UpdateStatus to Creating: %v", err)
	}
	if updated.Phase != vm.PhaseCreating {
		t.Fatalf("expected phase Creating, got %s", updated.Phase)
	}
	if updated.LastPowerAction != "define" {
		t.Fatalf("expected lastPowerAction 'define', got %s", updated.LastPowerAction)
	}

	// Creating -> Running
	updated, err = svc.UpdateStatus(ctx, "vm-test-01", vm.PhaseRunning, "start", "")
	if err != nil {
		t.Fatalf("UpdateStatus to Running: %v", err)
	}
	if updated.Phase != vm.PhaseRunning {
		t.Fatalf("expected phase Running, got %s", updated.Phase)
	}

	// Running -> Error with error message
	updated, err = svc.UpdateStatus(ctx, "vm-test-01", vm.PhaseError, "shutdown", "virsh shutdown failed")
	if err != nil {
		t.Fatalf("UpdateStatus to Error: %v", err)
	}
	if updated.Phase != vm.PhaseError {
		t.Fatalf("expected phase Error, got %s", updated.Phase)
	}
	if updated.LastError != "virsh shutdown failed" {
		t.Fatalf("expected lastError, got %s", updated.LastError)
	}
}

func TestServiceUpdateStatusNonExistent(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.UpdateStatus(ctx, "non-existent-vm", vm.PhaseRunning, "start", "")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceCreateForcesLibvirtPowerControl(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	v := testVM()
	// Even if powerControlMethod is empty, it should be forced to libvirt.
	v.PowerControlMethod = ""
	created, err := svc.Create(ctx, v)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.PowerControlMethod != vm.PowerControlLibvirt {
		t.Fatalf("expected powerControlMethod libvirt, got %s", created.PowerControlMethod)
	}
}

func TestServiceDeleteNonExistentVM(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	err := svc.Delete(ctx, "ghost-vm")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceGetNonExistentVM(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.Get(ctx, "ghost-vm")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceListEmpty(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 VMs, got %d", len(list))
	}
}

func TestServiceCreateSetsTimestamps(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, testVM())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("expected non-zero createdAt")
	}
	if created.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero updatedAt")
	}
	if created.Phase != vm.PhasePending {
		t.Fatalf("expected phase Pending, got %s", created.Phase)
	}
}

func TestUpdateDeployStatusPreservesCompletedProvisioning(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	created, err := svc.Create(ctx, testVM())
	if err != nil {
		t.Fatalf("create vm: %v", err)
	}

	// The provisioning window armed by the API handler before deploying.
	started := time.Now().UTC()
	deadline := started.Add(time.Hour)
	armed := vm.ProvisioningStatus{Active: true, StartedAt: &started, DeadlineAt: &deadline, CompletionToken: "tok-1"}
	created.Provisioning = armed
	if err := svc.Store().Upsert(ctx, created); err != nil {
		t.Fatalf("arm provisioning: %v", err)
	}

	// An install-complete callback wins the race while the deploy unwinds.
	completed := created
	completed.MarkProvisionComplete("callback", time.Now().UTC())
	if err := svc.Store().Upsert(ctx, completed); err != nil {
		t.Fatalf("complete provisioning: %v", err)
	}

	updated, err := svc.UpdateDeployStatus(ctx, created.Name, vm.PhaseProvisioning, "create+cloudimage", armed)
	if err != nil {
		t.Fatalf("UpdateDeployStatus: %v", err)
	}
	if updated.Provisioning.Active {
		t.Fatal("expected completed provisioning to stay inactive")
	}
	if updated.Provisioning.CompletedAt == nil {
		t.Fatal("expected completedAt to be preserved")
	}
	if updated.Phase != vm.PhaseRunning {
		t.Fatalf("expected phase to stay Running after completion, got %s", updated.Phase)
	}

	// A different completion token belongs to an older install; the new
	// deploy's window must still be restored.
	rearmed := armed
	rearmed.CompletionToken = "tok-2"
	restored, err := svc.UpdateDeployStatus(ctx, created.Name, vm.PhaseProvisioning, "redeploy", rearmed)
	if err != nil {
		t.Fatalf("UpdateDeployStatus new token: %v", err)
	}
	if !restored.Provisioning.Active || restored.Provisioning.CompletionToken != "tok-2" {
		t.Fatalf("expected new provisioning window to be restored, got %+v", restored.Provisioning)
	}
}

func TestUpdateDeployStatusPreservesDomainObservation(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	created, err := svc.Create(ctx, testVM())
	if err != nil {
		t.Fatalf("create vm: %v", err)
	}

	started := time.Now().UTC()
	deadline := started.Add(time.Hour)
	armed := vm.ProvisioningStatus{Active: true, StartedAt: &started, DeadlineAt: &deadline, CompletionToken: "tok-observe"}
	created.Provisioning = armed
	if err := svc.Store().Upsert(ctx, created); err != nil {
		t.Fatalf("arm provisioning: %v", err)
	}

	// The runtime sync loop observes the domain before the deploy unwinds.
	observed := started.Add(time.Minute)
	seen := created
	seen.Provisioning.DomainObservedAt = &observed
	if err := svc.Store().Upsert(ctx, seen); err != nil {
		t.Fatalf("record observation: %v", err)
	}

	restored, err := svc.UpdateDeployStatus(ctx, created.Name, vm.PhaseProvisioning, "create+cloudimage", armed)
	if err != nil {
		t.Fatalf("UpdateDeployStatus: %v", err)
	}
	if restored.Provisioning.DomainObservedAt == nil {
		t.Fatal("expected domain observation to survive the deploy status restore")
	}
}
