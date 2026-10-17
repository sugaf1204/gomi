package sql_test

import (
	"context"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/vm"
	"testing"
	"time"
)

func TestProvisioningAttemptCAS(t *testing.T) {
	sqlBackend := newTestBackend(t)
	memBackend := memory.New()
	for _, tt := range []struct {
		name     string
		machines machine.Store
		vms      vm.Store
	}{{"sql", sqlBackend.Machines(), sqlBackend.VMs()}, {"memory", memBackend.Machines(), memBackend.VMs()}} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now().UTC()
			v := vm.VirtualMachine{Name: "win-test", CreatedAt: now, UpdatedAt: now, Provisioning: vm.ProvisioningStatus{Active: true, CompletionToken: "token-1", AttemptID: "attempt-1"}}
			if err := tt.vms.Upsert(ctx, v); err != nil {
				t.Fatal(err)
			}
			before, err := tt.vms.Get(ctx, v.Name)
			if err != nil {
				t.Fatal(err)
			}
			after := before
			after.Provisioning.WindowsInstallStarted = true
			updater := tt.vms.(vm.ProvisioningAttemptUpdater)
			if ok, err := updater.UpdateProvisioningAttempt(ctx, before, after); err != nil || !ok {
				t.Fatalf("CAS failed: %v %v", ok, err)
			}
			if ok, err := updater.UpdateProvisioningAttempt(ctx, before, after); err != nil || ok {
				t.Fatalf("duplicate stage accepted: %v %v", ok, err)
			}
			newer := after
			newer.Provisioning = vm.ProvisioningStatus{Active: true, CompletionToken: "token-2", AttemptID: "attempt-2"}
			if err := tt.vms.Upsert(ctx, newer); err != nil {
				t.Fatal(err)
			}
			completed := after
			completed.Provisioning.Active = false
			if ok, err := updater.UpdateProvisioningAttempt(ctx, after, completed); err != nil || ok {
				t.Fatalf("old attempt accepted: %v %v", ok, err)
			}
			if err := tt.vms.Delete(ctx, v.Name); err != nil {
				t.Fatal(err)
			}
			if ok, err := updater.UpdateProvisioningAttempt(ctx, newer, newer); err != nil || ok {
				t.Fatalf("deleted VM resurrected: %v %v", ok, err)
			}

			m := machine.Machine{Name: "win-host", MAC: "52:54:00:11:22:33", CreatedAt: now, UpdatedAt: now, Provision: &machine.ProvisionProgress{Active: true, CompletionToken: "token-1", AttemptID: "attempt-1"}}
			if err := tt.machines.Upsert(ctx, m); err != nil {
				t.Fatal(err)
			}
			mbefore, err := tt.machines.Get(ctx, m.Name)
			if err != nil {
				t.Fatal(err)
			}
			mafter := mbefore
			progress := *mbefore.Provision
			progress.Artifacts = map[string]string{"windowsInstallStarted": "true"}
			mafter.Provision = &progress
			mu := tt.machines.(machine.ProvisioningAttemptUpdater)
			if ok, err := mu.UpdateProvisioningAttempt(ctx, mbefore, mafter); err != nil || !ok {
				t.Fatalf("machine CAS failed: %v %v", ok, err)
			}
			if ok, err := mu.UpdateProvisioningAttempt(ctx, mbefore, mafter); err != nil || ok {
				t.Fatalf("duplicate machine stage accepted: %v %v", ok, err)
			}
			if err := tt.machines.Delete(ctx, m.Name); err != nil {
				t.Fatal(err)
			}
			if ok, err := mu.UpdateProvisioningAttempt(ctx, mafter, mafter); err != nil || ok {
				t.Fatalf("deleted machine resurrected: %v %v", ok, err)
			}
		})
	}
}
