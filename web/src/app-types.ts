import type { PowerConfig } from './types'

export type LoadState = 'idle' | 'loading' | 'error'
export type View = 'overview' | 'machines' | 'hypervisors' | 'virtual-machines' | 'activity' | 'network' | 'dhcp-leases' | 'cloud-init' | 'os-images' | 'users' | 'settings'
export type GuardedAction = 'redeploy' | 'poweron' | 'poweroff' | 'delete'
export type MachineTab = 'info' | 'detail' | 'network' | 'console' | 'activity' | 'configuration'
export type ActivityType = 'all' | 'audit'
export type Theme = 'default' | 'rounded'

export type ActivityItem = {
  id: string
  type: 'audit'
  machine: string
  action: string
  actor: string
  result: string
  message?: string
  timestamp: string
}

export type ActivitySummary = {
  total: number
  auditCount: number
  success: number
  failure: number
}

export type ConfirmDialogState = {
  open: boolean
  action: GuardedAction
  machineName: string
  targets: string[]
  input: string
  running: boolean
}

export type MachineStats = {
  ready: number
  provisioning: number
  attention: number
}

export type MachineSettingsDraft = {
  power: PowerConfig
}

export type AuthFormState = {
  username: string
  password: string
}

export type SubnetFormState = {
  name: string
  cidr: string
  gateway: string
  dnsServers: string
  vlanId: string
}
