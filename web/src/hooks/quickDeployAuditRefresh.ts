import type { View } from '../app-types'

// refreshAll fetches the dashboard resources but not audit events, and the
// audit effect only fires when view/selectedMachine/activityMachineFilter
// change. A header deploy changes none of those, so the create-vm event the
// server writes would not reach an Activity timeline that is already open.
//
// Returns the machine filter to refetch with, or null when the current view
// does not show a feed the new VM's event belongs to:
//   - overview never fetches audit at all
//   - machines scopes its feed to the selected machine, and the new VM is not it
// An active machine filter is preserved: refetching unfiltered would replace a
// filtered timeline with every machine's events.
export function quickDeployAuditRefreshTarget(
  view: View,
  activityMachineFilter: string
): { machineName: string | undefined } | null {
  if (view !== 'activity') return null
  return { machineName: activityMachineFilter || undefined }
}
