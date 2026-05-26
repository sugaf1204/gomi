import type { VirtualMachine } from '../../../types'

export type CloudInitInputMode = 'none' | 'existing' | 'create'

export type VMConfigForm = {
  hypervisorRef: string
  cpuCores: string
  memoryMB: string
  diskGB: string
  osImageRef: string
  cloudInitMode: CloudInitInputMode
  cloudInitExistingRef: string
  cloudInitTemplateName: string
  cloudInitUserData: string
  cpuMode: '' | 'host-passthrough' | 'host-model' | 'maximum'
  diskDriver: string
  diskFormat: string
  ioThreads: string
  netMultiqueue: string
  cpuPinning: string
  subnetRef: string
  domain: string
  ipAssignment: 'dhcp' | 'static'
  staticIP: string
  bridge: string
  sshKeyRefs: string[]
  loginUserUsername: string
  loginUserPassword: string
  loginUserPasswordTouched: boolean
}

export type VMForm = VMConfigForm & {
  name: string
  count: string
}

export type VMReinstallForm = VMConfigForm

export type QuickDeployPreset = {
  name: string
  count: string
  hypervisorRef: string
  cpuCores: string
  memoryMB: string
  diskGB: string
  osImageRef: string
  subnetRef: string
  bridge: string
  ipAssignment: 'dhcp'
  cloudInitRefs: string[]
  sshKeyRefs: string[]
  loginUserUsername: string
  loginUserPassword: string
}

export type UpdateVMConfigForm = (updater: (current: VMConfigForm) => VMConfigForm) => void

export type VMPowerAction = 'power-on' | 'power-off'
export type VMPrimaryAction = 'console' | 'power-on' | 'power-off' | 'redeploy' | 'migrate' | 'delete'

export type VMPowerConfirmState = {
  open: boolean
  action: VMPowerAction
  targets: string[]
  running: boolean
}

export type VMDeleteConfirmState = {
  open: boolean
  targets: string[]
  running: boolean
}

export type VMBulkRedeployConfirmState = {
  open: boolean
  targets: string[]
  activeTarget: string
  forms: Record<string, VMReinstallForm>
  advancedOpen: Record<string, boolean>
  status: Record<string, { state: VMBulkRedeployTargetStatus; error?: string }>
  running: boolean
}

export type VMBulkRedeployTargetStatus = 'pending' | 'running' | 'succeeded' | 'failed'

export type VMMigrateConfirmState = {
  open: boolean
  vmName: string
  targetHypervisor: string
  running: boolean
}

export const QUICK_DEPLOY_STORAGE_KEY = 'gomi.virtual-machines.quick-deploy-preset'
export const VM_SELECTION_STORAGE_KEY = 'gomi.virtual-machines.selected'

export const initialVMConfigForm: VMConfigForm = {
  hypervisorRef: '',
  cpuCores: '2',
  memoryMB: '2048',
  diskGB: '20',
  osImageRef: '',
  cloudInitMode: 'none',
  cloudInitExistingRef: '',
  cloudInitTemplateName: '',
  cloudInitUserData: '',
  cpuMode: '',
  diskDriver: '',
  diskFormat: '',
  ioThreads: '',
  netMultiqueue: '',
  cpuPinning: '',
  subnetRef: '',
  domain: '',
  ipAssignment: 'dhcp',
  staticIP: '',
  bridge: '',
  sshKeyRefs: [],
  loginUserUsername: '',
  loginUserPassword: '',
  loginUserPasswordTouched: false
}

export const initialForm: VMForm = {
  name: '',
  count: '1',
  ...initialVMConfigForm
}

export const initialReinstallForm: VMReinstallForm = { ...initialVMConfigForm }

export const initialQuickDeployPreset: QuickDeployPreset = {
  name: '',
  count: '1',
  hypervisorRef: '',
  cpuCores: '2',
  memoryMB: '2048',
  diskGB: '20',
  osImageRef: '',
  subnetRef: '',
  bridge: '',
  ipAssignment: 'dhcp',
  cloudInitRefs: [],
  sshKeyRefs: [],
  loginUserUsername: '',
  loginUserPassword: ''
}

export const initialPowerConfirm: VMPowerConfirmState = {
  open: false,
  action: 'power-on',
  targets: [],
  running: false
}

export const initialDeleteConfirm: VMDeleteConfirmState = {
  open: false,
  targets: [],
  running: false
}

export const initialBulkRedeployConfirm: VMBulkRedeployConfirmState = {
  open: false,
  targets: [],
  activeTarget: '',
  forms: {},
  advancedOpen: {},
  status: {},
  running: false
}

export const initialMigrateConfirm: VMMigrateConfirmState = {
  open: false,
  vmName: '',
  targetHypervisor: '',
  running: false
}

export function readQuickDeployPreset(): QuickDeployPreset {
  if (typeof window === 'undefined') return initialQuickDeployPreset
  try {
    const raw = localStorage.getItem(QUICK_DEPLOY_STORAGE_KEY)
    if (!raw) return initialQuickDeployPreset
    const parsed = JSON.parse(raw) as Partial<QuickDeployPreset & { count: number }>
    return {
      ...initialQuickDeployPreset,
      ...parsed,
      name: typeof parsed.name === 'string' ? parsed.name : '',
      count: String(parsed.count ?? '1'),
      ipAssignment: 'dhcp',
      loginUserPassword: '',
      cloudInitRefs: Array.isArray(parsed.cloudInitRefs) ? parsed.cloudInitRefs.filter((ref): ref is string => typeof ref === 'string') : [],
      sshKeyRefs: Array.isArray(parsed.sshKeyRefs) ? parsed.sshKeyRefs.filter((ref): ref is string => typeof ref === 'string') : []
    }
  } catch {
    return initialQuickDeployPreset
  }
}

export function formatCPUPinning(cpuPinning?: Record<number, string>) {
  if (!cpuPinning) return ''
  return Object.entries(cpuPinning)
    .sort(([left], [right]) => Number(left) - Number(right))
    .map(([vcpu, cpuset]) => `${vcpu}:${cpuset}`)
    .join(',')
}

export function primaryCloudInitRef(vm: VirtualMachine): string {
  if (vm.lastDeployedCloudInitRef && vm.lastDeployedCloudInitRef.trim().length > 0) {
    return vm.lastDeployedCloudInitRef
  }
  return vm.cloudInitRefs?.[0] ?? vm.cloudInitRef ?? ''
}

export function currentVMCloudInitRefs(vm?: VirtualMachine) {
  const refs = vm?.cloudInitRefs?.map((ref) => ref.trim()).filter(Boolean) ?? []
  const legacyRef = vm?.cloudInitRef?.trim() ?? ''
  const primaryRef = vm ? primaryCloudInitRef(vm).trim() : ''
  const ordered = primaryRef ? [primaryRef, ...refs] : refs
  if (legacyRef) ordered.push(legacyRef)
  return Array.from(new Set(ordered.filter(Boolean)))
}

export function mergeSelectedCloudInitRef(selectedRef: string, currentRefs: string[]) {
  const trimmedRef = selectedRef.trim()
  if (!trimmedRef) return currentRefs
  return [trimmedRef, ...currentRefs.filter((ref) => ref !== trimmedRef)]
}

export function toReinstallForm(vm: VirtualMachine): VMReinstallForm {
  const nic = vm.network?.[0]
  const cloudInitRef = primaryCloudInitRef(vm)
  return {
    hypervisorRef: vm.hypervisorRef || '',
    cpuCores: String(vm.resources.cpuCores || 2),
    memoryMB: String(vm.resources.memoryMB || 2048),
    diskGB: String(vm.resources.diskGB || 20),
    osImageRef: vm.osImageRef || '',
    cloudInitMode: cloudInitRef ? 'existing' : 'none',
    cloudInitExistingRef: cloudInitRef,
    cloudInitTemplateName: '',
    cloudInitUserData: '',
    cpuMode: vm.advancedOptions?.cpuMode || '',
    diskDriver: vm.advancedOptions?.diskDriver || '',
    diskFormat: vm.advancedOptions?.diskFormat || '',
    ioThreads: vm.advancedOptions?.ioThreads ? String(vm.advancedOptions.ioThreads) : '',
    netMultiqueue: vm.advancedOptions?.netMultiqueue ? String(vm.advancedOptions.netMultiqueue) : '',
    cpuPinning: formatCPUPinning(vm.advancedOptions?.cpuPinning),
    subnetRef: vm.subnetRef || nic?.network || '',
    domain: vm.domain || '',
    ipAssignment: vm.ipAssignment === 'static' ? 'static' : 'dhcp',
    staticIP: nic?.ipAddress || '',
    bridge: nic?.bridge || '',
    sshKeyRefs: vm.sshKeyRefs ?? [],
    loginUserUsername: vm.loginUser?.username || '',
    loginUserPassword: '',
    loginUserPasswordTouched: false
  }
}

function parseCPUPinning(raw: string): Record<number, string> | undefined {
  const trimmed = raw.trim()
  if (!trimmed) return undefined
  const result: Record<number, string> = {}
  for (const pair of trimmed.split(',')) {
    const [vcpu, cpuset] = pair.split(':').map((s) => s.trim())
    const vcpuNum = Number(vcpu)
    if (Number.isNaN(vcpuNum) || !cpuset) continue
    result[vcpuNum] = cpuset
  }
  return Object.keys(result).length > 0 ? result : undefined
}

export function buildAdvancedOptions(form: Pick<VMConfigForm, 'cpuMode' | 'diskDriver' | 'diskFormat' | 'ioThreads' | 'netMultiqueue' | 'cpuPinning'>): VirtualMachine['advancedOptions'] {
  const cpuMode = form.cpuMode as '' | 'host-passthrough' | 'host-model' | 'maximum'
  const diskDriver = form.diskDriver as 'virtio' | 'scsi' | ''
  const diskFormat = form.diskFormat as 'qcow2' | ''
  const ioThreads = Number(form.ioThreads) || 0
  const netMultiqueue = Number(form.netMultiqueue) || 0
  const cpuPinning = parseCPUPinning(form.cpuPinning)

  if (!cpuMode && !diskDriver && !diskFormat && !ioThreads && !netMultiqueue && !cpuPinning) {
    return undefined
  }

  return {
    ...(cpuMode ? { cpuMode } : {}),
    ...(diskDriver ? { diskDriver } : {}),
    ...(diskFormat ? { diskFormat } : {}),
    ...(ioThreads > 0 ? { ioThreads } : {}),
    ...(netMultiqueue > 0 ? { netMultiqueue } : {}),
    ...(cpuPinning ? { cpuPinning } : {})
  }
}

export function buildVMLoginUserPayload(
  formState: Pick<VMConfigForm, 'loginUserUsername' | 'loginUserPassword' | 'loginUserPasswordTouched'>,
  currentLoginUser?: VirtualMachine['loginUser']
): VirtualMachine['loginUser'] | undefined {
  const username = formState.loginUserUsername.trim()
  const password = formState.loginUserPassword.trim()
  if (!username) return undefined
  if (currentLoginUser?.username === username && !password && !formState.loginUserPasswordTouched) return undefined
  return {
    username,
    ...(password ? { password } : {})
  }
}
