package pxehttp

import (
	"context"
	"fmt"
	"time"

	"github.com/sugaf1204/gomi/internal/machine"
)

// Only restoration evidence may arrive after a sealed attempt expires. It does
// not reactivate the deployment or mark it complete; it permits explicit recovery.
func (h *Handler) updateProvisionEvent(ctx context.Context, expected *machine.Machine, eventType string, fn func(*machine.Machine)) error {
	fresh, err := h.machines.Get(ctx, expected.Name)
	if err != nil {
		return err
	}
	if fresh.Provision == nil || expected.Provision == nil ||
		fresh.Provision.AttemptID != expected.Provision.AttemptID ||
		fresh.Provision.CompletionToken != expected.Provision.CompletionToken {
		return fmt.Errorf("provisioning attempt changed")
	}
	if !fresh.Provision.Active && (eventType != deployEventImageApplied || fresh.SealedBootstrap == nil || fresh.SealedBootstrap.Owner == "") {
		return fmt.Errorf("provisioning attempt is inactive")
	}
	fn(&fresh)
	fresh.UpdatedAt = time.Now().UTC()
	// The SQL store also rejects writes if an enrolled host's attempt changed
	// after this read, preventing a late acknowledgement from marking a retry.
	return h.machines.Store().Upsert(ctx, fresh)
}
