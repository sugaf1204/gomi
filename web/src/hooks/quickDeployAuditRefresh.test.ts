import { describe, expect, it } from 'vitest'
import { quickDeployAuditRefreshTarget } from './quickDeployAuditRefresh'
import type { View } from '../app-types'

describe('quickDeployAuditRefreshTarget', () => {
  // The regression this guards: deploying from the header while the Activity
  // view is open left the server's create-vm event off the visible timeline,
  // because refreshAll does not fetch audit and no view/filter change fires
  // the audit effect.
  it('refetches the unfiltered feed on the activity view', () => {
    expect(quickDeployAuditRefreshTarget('activity', '')).toEqual({ machineName: undefined })
  })

  // Refetching unfiltered here would swap a machine-scoped timeline for every
  // machine's events, which the user did not ask for.
  it('preserves an active machine filter', () => {
    expect(quickDeployAuditRefreshTarget('activity', 'node-01')).toEqual({ machineName: 'node-01' })
  })

  // The machines view scopes its feed to the selected machine, and a freshly
  // created VM is never that machine; the overview never fetches audit at all.
  it.each(['overview', 'machines', 'virtual-machines'] as const)('skips the refetch on the %s view', (view) => {
    expect(quickDeployAuditRefreshTarget(view, '')).toBeNull()
  })

  it('skips every view that does not render the activity feed', () => {
    const views: View[] = [
      'overview', 'machines', 'hypervisors', 'virtual-machines', 'network',
      'dns-records', 'cloud-init', 'os-images', 'users', 'settings'
    ]
    for (const view of views) {
      expect(quickDeployAuditRefreshTarget(view, 'node-01')).toBeNull()
    }
  })
})
