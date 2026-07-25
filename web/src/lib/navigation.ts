import type { View } from '../app-types'

export type NavItem = {
  view: View
  label: string
  /** Which count, if any, the rail shows right-aligned on this row. */
  count?: 'machines' | 'virtualMachines' | 'hypervisors' | 'subnets' | 'osImages' | 'cloudInits' | 'dnsRecords' | 'dhcpLeases'
}

export type NavGroup = {
  heading: string
  items: NavItem[]
}

/** Sits alone above the labelled groups. */
export const PRIMARY_ITEM: NavItem = { view: 'overview', label: 'Overview' }

/** The three daily-use tiers of the rail. */
export const NAV_GROUPS: NavGroup[] = [
  {
    heading: 'FLEET',
    items: [
      { view: 'machines', label: 'Machines', count: 'machines' },
      { view: 'virtual-machines', label: 'Virtual Machines', count: 'virtualMachines' },
      { view: 'hypervisors', label: 'Hypervisors', count: 'hypervisors' },
    ],
  },
  {
    heading: 'CATALOG',
    items: [
      { view: 'os-images', label: 'OS Images', count: 'osImages' },
      { view: 'cloud-init', label: 'Cloud-Init', count: 'cloudInits' },
    ],
  },
  {
    heading: 'NETWORK',
    items: [
      { view: 'network', label: 'Subnets', count: 'subnets' },
      { view: 'dns-records', label: 'DNS Records', count: 'dnsRecords' },
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
  return [PRIMARY_ITEM, ...NAV_GROUPS.flatMap((group) => group.items), ...SYSTEM_ITEMS]
}
