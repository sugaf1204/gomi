package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

func createCascadeHypervisor(t *testing.T, env testEnv, name, machineRef string) {
	t.Helper()
	if _, err := env.hypervisors.Create(context.Background(), hypervisor.Hypervisor{
		Name:       name,
		MachineRef: machineRef,
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "127.0.0.1",
			Port: 16509,
		},
		Phase: hypervisor.PhaseReady,
	}); err != nil {
		t.Fatalf("create hypervisor %s: %v", name, err)
	}
}

func createCascadeVM(t *testing.T, env testEnv, name, hypervisorRef string, phase vm.Phase) {
	t.Helper()
	ctx := context.Background()
	created, err := env.vms.Create(ctx, vm.VirtualMachine{
		Name:          name,
		HypervisorRef: hypervisorRef,
		Resources:     vm.ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
		OSImageRef:    "ubuntu-test",
	})
	if err != nil {
		t.Fatalf("create vm %s: %v", name, err)
	}
	if phase != "" && created.Phase != phase {
		created.Phase = phase
		if err := env.vms.Store().Upsert(ctx, created); err != nil {
			t.Fatalf("set vm %s phase: %v", name, err)
		}
	}
}

func requireVMGone(t *testing.T, env testEnv, name string) {
	t.Helper()
	if _, err := env.vms.Get(context.Background(), name); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected vm %s to be deleted, got err=%v", name, err)
	}
}

func TestDeleteMissingVMAttemptsRuntimeTeardown(t *testing.T) {
	// A domain may have been recreated on the host after the VM was marked
	// Missing; delete must still attempt the not-found-tolerant teardown.
	calls := 0
	env := setupTestEnvWithVMRuntimeDeleter(t, func(context.Context, vm.VirtualMachine) error {
		calls++
		return nil
	})
	createCascadeHypervisor(t, env, "hv-missing-del", "")
	createCascadeVM(t, env, "vm-missing-del", "hv-missing-del", vm.PhaseMissing)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-missing-del", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)
	requireVMGone(t, env, "vm-missing-del")
	if calls != 1 {
		t.Fatalf("expected runtime teardown to be attempted once for Missing vm, got %d", calls)
	}
}

func TestDeleteMissingVMFallsBackToRecordOnlyWhenTeardownNotAttempted(t *testing.T) {
	env := setupTestEnvWithVMRuntimeDeleter(t, func(context.Context, vm.VirtualMachine) error {
		return fmt.Errorf("connect to hypervisor: %w", infraapi.ErrVMTeardownNotAttempted)
	})
	createCascadeHypervisor(t, env, "hv-missing-fb", "")
	createCascadeVM(t, env, "vm-missing-fb", "hv-missing-fb", vm.PhaseMissing)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-missing-fb", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)
	requireVMGone(t, env, "vm-missing-fb")
}

func TestDeleteMissingVMKeepsRecordOnPartialTeardownFailure(t *testing.T) {
	// A teardown that already started mutating the host (e.g. destroy
	// succeeded, undefine failed) must keep the record so cleanup can retry.
	env := setupTestEnvWithVMRuntimeDeleter(t, func(context.Context, vm.VirtualMachine) error {
		return errors.New("undefine domain vm-missing-partial before delete: rpc failed")
	})
	createCascadeHypervisor(t, env, "hv-missing-partial", "")
	createCascadeVM(t, env, "vm-missing-partial", "hv-missing-partial", vm.PhaseMissing)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-missing-partial", nil, env.token)
	requireStatus(t, rec, http.StatusBadGateway)
	if _, err := env.vms.Get(context.Background(), "vm-missing-partial"); err != nil {
		t.Fatalf("expected vm record to survive partial teardown failure: %v", err)
	}
}

func TestDeleteNonMissingVMStillBlocksOnTeardownFailure(t *testing.T) {
	env := setupTestEnvWithVMRuntimeDeleter(t, func(context.Context, vm.VirtualMachine) error {
		return errors.New("hypervisor unreachable")
	})
	createCascadeHypervisor(t, env, "hv-live-block", "")
	createCascadeVM(t, env, "vm-live-block", "hv-live-block", vm.PhaseRunning)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-live-block", nil, env.token)
	requireStatus(t, rec, http.StatusBadGateway)
	if _, err := env.vms.Get(context.Background(), "vm-live-block"); err != nil {
		t.Fatalf("expected vm record to survive blocked delete: %v", err)
	}
}

func TestDeleteVMWithDanglingHypervisorRefDeletesRecord(t *testing.T) {
	// No override: exercise the real runtime path, which must skip teardown
	// when the hypervisor record does not exist.
	env := setupTestEnvWithVMRuntimeDeleter(t, nil)
	createCascadeVM(t, env, "vm-dangling", "hv-gone", "")

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-dangling", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)
	requireVMGone(t, env, "vm-dangling")
}

func TestDeleteHypervisorCascadesVMRecords(t *testing.T) {
	env := setupTestEnv(t)
	createCascadeHypervisor(t, env, "hv-cascade", "")
	createCascadeVM(t, env, "vm-cascade-a", "hv-cascade", "")
	createCascadeVM(t, env, "vm-cascade-b", "hv-cascade", "")
	createCascadeVM(t, env, "vm-other", "hv-other", "")

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/hypervisors/hv-cascade", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	if _, err := env.hypervisors.Get(context.Background(), "hv-cascade"); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected hypervisor to be deleted, got err=%v", err)
	}
	requireVMGone(t, env, "vm-cascade-a")
	requireVMGone(t, env, "vm-cascade-b")
	if _, err := env.vms.Get(context.Background(), "vm-other"); err != nil {
		t.Fatalf("vm on another hypervisor must survive: %v", err)
	}
}

func TestDeleteMachineCascadesHypervisorAndVMRecords(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	if err := env.machines.Store().Upsert(ctx, machine.Machine{
		Name:     "machine-cascade",
		Hostname: "machine-cascade",
		MAC:      "aa:bb:cc:dd:ee:01",
	}); err != nil {
		t.Fatalf("seed machine: %v", err)
	}
	createCascadeHypervisor(t, env, "hv-of-machine", "machine-cascade")
	createCascadeVM(t, env, "vm-of-machine-a", "hv-of-machine", "")
	createCascadeVM(t, env, "vm-of-machine-b", "hv-of-machine", "")

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/machines/machine-cascade", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	if _, err := env.machines.Get(ctx, "machine-cascade"); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected machine to be deleted, got err=%v", err)
	}
	if _, err := env.hypervisors.Get(ctx, "hv-of-machine"); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected linked hypervisor to be deleted, got err=%v", err)
	}
	requireVMGone(t, env, "vm-of-machine-a")
	requireVMGone(t, env, "vm-of-machine-b")
}

func TestDeleteMachineWithLinkedHypervisorRequiresAdmin(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	createUser(t, env.authStore, "operator1", "operatorpass", auth.RoleOperator)
	operatorToken := createSession(t, env.authStore, "operator1")

	if err := env.machines.Store().Upsert(ctx, machine.Machine{
		Name:     "machine-rbac",
		Hostname: "machine-rbac",
		MAC:      "aa:bb:cc:dd:ee:02",
	}); err != nil {
		t.Fatalf("seed machine: %v", err)
	}
	createCascadeHypervisor(t, env, "hv-rbac", "machine-rbac")

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/machines/machine-rbac", nil, operatorToken)
	requireStatus(t, rec, http.StatusForbidden)
	if _, err := env.machines.Get(ctx, "machine-rbac"); err != nil {
		t.Fatalf("expected machine to survive forbidden delete: %v", err)
	}
	if _, err := env.hypervisors.Get(ctx, "hv-rbac"); err != nil {
		t.Fatalf("expected hypervisor to survive forbidden delete: %v", err)
	}

	// A machine without linked hypervisors stays deletable by an operator.
	if err := env.machines.Store().Upsert(ctx, machine.Machine{
		Name:     "machine-plain",
		Hostname: "machine-plain",
		MAC:      "aa:bb:cc:dd:ee:03",
	}); err != nil {
		t.Fatalf("seed plain machine: %v", err)
	}
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/machines/machine-plain", nil, operatorToken)
	requireStatus(t, rec, http.StatusNoContent)

	// The admin token deletes the linked machine with the cascade.
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/machines/machine-rbac", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)
	if _, err := env.hypervisors.Get(ctx, "hv-rbac"); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected linked hypervisor to be deleted by admin cascade, got err=%v", err)
	}
}
