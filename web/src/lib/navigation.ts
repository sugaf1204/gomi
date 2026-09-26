import type { View } from '../app-types'

export type NavItem = {
  view: View
  label: string
}

export type NavGroup = {
  heading: string
  items: NavItem[]
}

/** The three daily-use tiers of the rail. */
export const NAV_GROUPS: NavGroup[] = [
  {
    heading: 'COMPUTE',
    items: [
      { view: 'machines', label: 'Machines' },
      { view: 'virtual-machines', label: 'Virtual Machines' },
      { view: 'hypervisors', label: 'Hypervisors' },
    ],
  },
  {
    heading: 'IMAGE',
    items: [
      { view: 'os-images', label: 'OS Images' },
      { view: 'cloud-init', label: 'Cloud-Init' },
    ],
  },
  {
    heading: 'NETWORK',
    items: [
      { view: 'network', label: 'Subnets' },
      { view: 'dns-records', label: 'DNS Records' },
    ],
  },
]

/** Rarely-used destinations, collapsed into the System disclosure. */
export const SYSTEM_ITEMS: NavItem[] = [
  { view: 'activity', label: 'Activity' },
  { view: 'users', label: 'Users' },
  { view: 'settings', label: 'Settings' },
]

const SYSTEM_VIEWS = new Set<View>(SYSTEM_ITEMS.map((item) => item.view))

/** True when the current view lives inside the System disclosure. */
export function isSystemView(view: View): boolean {
  return SYSTEM_VIEWS.has(view)
}

/** Every destination, flattened — used by the jump-to palette. */
export function allNavItems(): NavItem[] {
  return [...NAV_GROUPS.flatMap((group) => group.items), ...SYSTEM_ITEMS]
}
