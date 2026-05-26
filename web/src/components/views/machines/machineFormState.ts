import type { Machine, PowerConfig, PowerType, Subnet } from '../../../types'

export type CloudInitInputMode = 'none' | 'existing' | 'create'
export type MachinePrimaryAction = 'power-on' | 'power-off' | 'redeploy' | 'delete'
export type BatchPowerAction = 'power-on' | 'power-off'
export type BatchRedeployTargetStatus = 'pending' | 'running' | 'succeeded' | 'failed'
export type MachineDialogMode = 'create' | 'redeploy'
export type UpdateMachineForm = (updater: (current: MachineFormState) => MachineFormState) => void

export type BatchPowerConfirmState = {
  open: boolean
  action: BatchPowerAction
  targets: string[]
  running: boolean
}

export type BatchDeleteConfirmState = {
  open: boolean
  targets: string[]
  running: boolean
}

export type BatchRedeployConfirmState = {
  open: boolean
  targets: string[]
  activeTarget: string
  forms: Record<string, MachineFormState>
  status: Record<string, { state: BatchRedeployTargetStatus; error?: string }>
  running: boolean
}

export type MachineFormState = {
  hostname: string
  mac: string
  powerType: PowerType
  ipmiHost: string
  ipmiUsername: string
  ipmiPassword: string
  webhookOnURL: string
  webhookOffURL: string
  webhookStatusURL: string
  webhookBootOrderURL: string
  wolMAC: string
  arch: string
  firmware: 'uefi' | 'bios'
  imageRef: string
  osFamily: string
  osVersion: string
  targetDisk: string
  cloudInitMode: CloudInitInputMode
  cloudInitExistingRef: string
  cloudInitTemplateName: string
  cloudInitUserData: string
  subnetRef: string
  ipAssignment: 'dhcp' | 'static'
  staticIP: string
  domain: string
  isHypervisor: boolean
  bridgeName: string
  sshKeyRefs: string[]
  loginUserUsername: string
  loginUserPassword: string
  loginUserPasswordTouched: boolean
}

export type MachineQuickDeployPreset = Omit<
  MachineFormState,
  'hostname' | 'cloudInitMode' | 'cloudInitTemplateName' | 'cloudInitUserData' | 'loginUserPasswordTouched'
> & {
  name: string
  count: string
}

export type MachineDialogState = {
  open: boolean
  mode: MachineDialogMode
  machineName: string
  running: boolean
}

export const MACHINE_QUICK_DEPLOY_STORAGE_KEY = 'gomi.machines.quick-deploy-preset'

export const initialBatchPowerConfirmState: BatchPowerConfirmState = {
  open: false,
  action: 'power-on',
  targets: [],
  running: false
}

export const initialBatchDeleteConfirmState: BatchDeleteConfirmState = {
  open: false,
  targets: [],
  running: false
}

export const initialMachineDialogState: MachineDialogState = {
  open: false,
  mode: 'create',
  machineName: '',
  running: false
}

export const initialBatchRedeployConfirmState: BatchRedeployConfirmState = {
  open: false,
  targets: [],
  activeTarget: '',
  forms: {},
  status: {},
  running: false
}

export function defaultSubnet(subnets: Subnet[]) {
  return subnets[0] ?? null
}

export function createInitialMachineForm(subnets: Subnet[]): MachineFormState {
  const subnet = defaultSubnet(subnets)
  return {
    hostname: '',
    mac: '',
    powerType: 'manual',
    ipmiHost: '',
    ipmiUsername: '',
    ipmiPassword: '',
    webhookOnURL: '',
    webhookOffURL: '',
    webhookStatusURL: '',
    webhookBootOrderURL: '',
    wolMAC: '',
    arch: 'amd64',
    firmware: 'uefi',
    imageRef: '',
    osFamily: '',
    osVersion: '',
    targetDisk: '',
    cloudInitMode: 'none',
    cloudInitExistingRef: '',
    cloudInitTemplateName: '',
    cloudInitUserData: '',
    subnetRef: subnet?.name ?? '',
    ipAssignment: 'dhcp',
    staticIP: '',
    domain: subnet?.spec.domainName ?? '',
    isHypervisor: false,
    bridgeName: 'br0',
    sshKeyRefs: [],
    loginUserUsername: '',
    loginUserPassword: '',
    loginUserPasswordTouched: false
  }
}

export function createInitialMachineQuickDeployPreset(subnets: Subnet[]): MachineQuickDeployPreset {
  const form = createInitialMachineForm(subnets)
  return {
    ...form,
    name: '',
    count: '1',
    cloudInitExistingRef: '',
    loginUserPassword: ''
  }
}

export function readMachineQuickDeployPreset(subnets: Subnet[]): MachineQuickDeployPreset {
  const initial = createInitialMachineQuickDeployPreset(subnets)
  if (typeof window === 'undefined') return initial
  try {
    const raw = localStorage.getItem(MACHINE_QUICK_DEPLOY_STORAGE_KEY)
    if (!raw) return initial
    const parsed = JSON.parse(raw) as Partial<MachineQuickDeployPreset & { count: number }>
    return {
      ...initial,
      ...parsed,
      name: typeof parsed.name === 'string' ? parsed.name : '',
      count: String(parsed.count ?? '1'),
      mac: typeof parsed.mac === 'string' ? parsed.mac : '',
      ipmiPassword: '',
      loginUserPassword: '',
      sshKeyRefs: Array.isArray(parsed.sshKeyRefs) ? parsed.sshKeyRefs.filter((ref): ref is string => typeof ref === 'string') : []
    }
  } catch {
    return initial
  }
}

export function currentMachineCloudInitRef(machine: Machine | null) {
  if (!machine) return ''
  if (machine.lastDeployedCloudInitRef?.trim()) return machine.lastDeployedCloudInitRef.trim()
  if (machine.cloudInitRefs?.[0]?.trim()) return machine.cloudInitRefs[0].trim()
  return ''
}

export function currentMachineCloudInitRefs(machine?: Machine) {
  const refs = machine?.cloudInitRefs?.map((ref) => ref.trim()).filter(Boolean) ?? []
  const primaryRef = machine ? currentMachineCloudInitRef(machine).trim() : ''
  if (primaryRef && !refs.includes(primaryRef)) {
    return [primaryRef, ...refs]
  }
  return refs
}

export function mergeSelectedCloudInitRef(selectedRef: string, currentRefs: string[]) {
  const trimmedRef = selectedRef.trim()
  if (!trimmedRef) return currentRefs
  return [trimmedRef, ...currentRefs.filter((ref) => ref !== trimmedRef)]
}

export function createMachineFormFromMachine(machine: Machine, subnets: Subnet[]): MachineFormState {
  const currentSubnet = subnets.find((subnet) => subnet.name === machine.subnetRef) ?? defaultSubnet(subnets)
  const currentCloudInitRef = currentMachineCloudInitRef(machine)
  return {
    hostname: machine.hostname || machine.name,
    mac: machine.mac || '',
    powerType: machine.power?.type || 'manual',
    ipmiHost: machine.power?.ipmi?.host || '',
    ipmiUsername: machine.power?.ipmi?.username || '',
    ipmiPassword: '',
    webhookOnURL: machine.power?.webhook?.powerOnURL || '',
    webhookOffURL: machine.power?.webhook?.powerOffURL || '',
    webhookStatusURL: machine.power?.webhook?.statusURL || '',
    webhookBootOrderURL: machine.power?.webhook?.bootOrderURL || '',
    wolMAC: machine.power?.wol?.wakeMAC || '',
    arch: machine.arch || 'amd64',
    firmware: machine.firmware || 'uefi',
    imageRef: machine.osPreset.imageRef || '',
    osFamily: machine.osPreset.family || '',
    osVersion: machine.osPreset.version || '',
    targetDisk: machine.targetDisk || '',
    cloudInitMode: currentCloudInitRef ? 'existing' : 'none',
    cloudInitExistingRef: currentCloudInitRef,
    cloudInitTemplateName: '',
    cloudInitUserData: '',
    subnetRef: machine.subnetRef || currentSubnet?.name || '',
    ipAssignment: machine.ipAssignment === 'static' ? 'static' : 'dhcp',
    staticIP: machine.ip || '',
    domain: machine.network?.domain || (machine.subnetRef ? currentSubnet?.spec.domainName || '' : ''),
    isHypervisor: machine.role === 'hypervisor',
    bridgeName: machine.bridgeName || 'br0',
    sshKeyRefs: machine.sshKeyRefs ?? [],
    loginUserUsername: machine.loginUser?.username || '',
    loginUserPassword: '',
    loginUserPasswordTouched: false
  }
}

export function buildPowerConfig(form: MachineFormState): PowerConfig {
  switch (form.powerType) {
    case 'ipmi':
      return { type: 'ipmi', ipmi: { host: form.ipmiHost, username: form.ipmiUsername, password: form.ipmiPassword } }
    case 'webhook':
      return {
        type: 'webhook',
        webhook: {
          powerOnURL: form.webhookOnURL,
          powerOffURL: form.webhookOffURL,
          statusURL: form.webhookStatusURL || undefined,
          bootOrderURL: form.webhookBootOrderURL || undefined,
        },
      }
    case 'wol':
      return { type: 'wol', wol: { wakeMAC: form.wolMAC || form.mac } }
    case 'manual':
    default:
      return { type: 'manual' }
  }
}

export function buildMachineLoginUserPayload(
  formState: Pick<MachineFormState, 'loginUserUsername' | 'loginUserPassword' | 'loginUserPasswordTouched'>,
  currentLoginUser?: Machine['loginUser']
): Machine['loginUser'] | undefined {
  const username = formState.loginUserUsername.trim()
  const password = formState.loginUserPassword.trim()
  if (!username) return undefined
  if (currentLoginUser?.username === username && !password && !formState.loginUserPasswordTouched) return undefined
  return {
    username,
    ...(password ? { password } : {})
  }
}
