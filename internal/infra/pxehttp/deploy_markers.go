package pxehttp

import (
	"context"
	"log"
	"time"

	"github.com/sugaf1204/gomi/internal/machine"
)

// appendMachineTimingOnce records a provision timing for the machine unless an
// event with the same name already exists in the current attempt. Boot scripts
// and config endpoints may be fetched repeatedly (firmware retries, iPXE
// chainloads), so dedup by name keeps one boundary marker per attempt.
// Recording is best-effort: errors are logged and never propagate, because a
// store failure must not block the boot-critical HTTP path.
func (h *Handler) appendMachineTimingOnce(ctx context.Context, m *machine.Machine, timing machine.ProvisionTiming) {
	if m == nil || m.Provision == nil || !m.Provision.Active {
		return
	}
	if hasProvisionTiming(m.Provision.Timings, timing.Name) {
		return
	}
	if err := h.updateProvisionProgress(ctx, m.Name, func(fresh *machine.Machine) {
		if fresh.Provision == nil || !fresh.Provision.Active {
			return
		}
		if hasProvisionTiming(fresh.Provision.Timings, timing.Name) {
			return
		}
		fresh.Provision.Timings = appendProvisionTiming(fresh.Provision.Timings, timing)
	}); err != nil {
		log.Printf("pxe: machine=%s record timing %s: %v", m.Name, timing.Name, err)
	}
}

func hasProvisionTiming(events []machine.ProvisionTiming, name string) bool {
	for _, event := range events {
		if event.Name == name {
			return true
		}
	}
	return false
}

func markerTiming(name, message string, at time.Time) machine.ProvisionTiming {
	return machine.ProvisionTiming{
		Source:    "server",
		Name:      name,
		EventType: "marker",
		Message:   message,
		Timestamp: &at,
	}
}

// recordPXEBootScriptMarker stamps the moment a provisioning machine fetched
// its iPXE boot script. The install-script fetch bounds the power-on/firmware
// PXE window; the local-boot fetch after image apply bounds the reboot window.
func (h *Handler) recordPXEBootScriptMarker(ctx context.Context, target pxeTarget, installScript bool) {
	m, ok := target.node.(*machine.Machine)
	if !ok {
		return
	}
	now := time.Now().UTC()
	if installScript {
		h.appendMachineTimingOnce(ctx, m, markerTiming(serverTimingPXEBootScript, "PXE boot script served (installer)", now))
		return
	}
	if machineImageApplied(m) {
		h.appendMachineTimingOnce(ctx, m, markerTiming(serverTimingPXEBootScriptLocalBoot, "PXE boot script served (local boot after image apply)", now))
	}
}
