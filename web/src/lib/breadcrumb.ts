import type { View } from '../app-types'

export type CrumbKind = 'ancestor' | 'current' | 'object'

export type Crumb = {
  label: string
  kind: CrumbKind
}

// Each view's place in the Fleet Rail hierarchy: the rail groups views under
// FLEET / CATALOG / NETWORK, and the breadcrumb shows that same parentage so
// the current object's position is always visible.
const VIEW_PATHS: Record<View, string[]> = {
  overview: ['overview'],
  machines: ['fleet', 'machines'],
  'virtual-machines': ['fleet', 'virtual machines'],
  hypervisors: ['fleet', 'hypervisors'],
  'os-images': ['catalog', 'os images'],
  'cloud-init': ['catalog', 'cloud-init'],
  network: ['network', 'subnets'],
  'dns-records': ['network', 'dns records'],
  activity: ['system', 'activity'],
  users: ['system', 'users'],
  settings: ['system', 'settings'],
}

/**
 * Builds the breadcrumb trail for a view, optionally ending in a selected
 * object. Ancestors render muted, the current segment in primary ink, and a
 * selected object in the brand accent.
 */
export function buildBreadcrumb(view: View, selectedObject?: string): Crumb[] {
  const segments = VIEW_PATHS[view] ?? [view]
  const crumbs: Crumb[] = segments.map((label, index) => ({
    label,
    kind: index === segments.length - 1 ? 'current' : 'ancestor',
  }))

  if (selectedObject) {
    return [...crumbs.map((crumb) => ({ ...crumb, kind: 'ancestor' as const })), { label: selectedObject, kind: 'object' }]
  }
  return crumbs
}
