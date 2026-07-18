package api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/sugaf1204/gomi/internal/hypervisor"
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

func TestDeleteMissingVMSkipsRuntimeTeardown(t *testing.T) {
	env := setupTestEnvWithVMRuntimeDeleter(t, func(_ context.Context, v vm.VirtualMachine) error {
		t.Errorf("runtime deleter must not be called for Missing vm %s", v.Name)
		return errors.New("unexpected runtime teardown")
	})
	createCascadeHypervisor(t, env, "hv-missing-del", "")
	createCascadeVM(t, env, "vm-missing-del", "hv-missing-del", vm.PhaseMissing)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-missing-del", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)
	requireVMGone(t, env, "vm-missing-del")
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
