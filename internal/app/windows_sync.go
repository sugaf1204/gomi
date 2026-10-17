package app

import (
	"context"
	"log"
	"time"

	"github.com/sugaf1204/gomi/internal/machine"
)

// Windows artifacts and progress belong to its attempt-scoped WinPE protocol.
// The periodic reconciler only expires its deadline, atomically, so concurrent
// completion, install claims, and new attempts cannot be overwritten.
func (r *Runtime) syncWindowsProvisioningState(ctx context.Context, before machine.Machine, now time.Time) {
	if before.Provision == nil || !before.Provision.Active || before.Provision.DeadlineAt == nil || !now.After(*before.Provision.DeadlineAt) {
		return
	}
	updater, ok := r.machineStore.(machine.ProvisioningAttemptUpdater)
	if !ok {
		log.Printf("sync: Windows timeout requires atomic provisioning updates")
		return
	}
	after := before
	progress := *before.Provision
	after.Provision = &progress
	after.Provision.Active = false
	after.Phase = machine.PhaseError
	after.LastError = "provisioning timed out"
	after.UpdatedAt = now
	if _, err := updater.UpdateProvisioningAttempt(ctx, before, after); err != nil {
		log.Printf("sync: Windows timeout save error: %v", err)
	}
}
