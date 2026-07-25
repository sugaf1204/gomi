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

// The preset carries exactly the fields a normal VM create carries, so any
// field added to the create form becomes settable in the preset for free.
export type QuickDeployPreset = VMConfigForm & {
  name: string
  count: string
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
  ...initialVMConfigForm
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

type StoredQuickDeployPreset = Partial<QuickDeployPreset> & {
  count?: string | number
  // Legacy shape: the preset used to select multiple Cloud-Init templates.
  cloudInitRefs?: unknown
}

// Legacy presets selected multiple Cloud-Init templates. The unified form models
// a single template, so the first entry wins and the rest are dropped.
function migrateCloudInitSelection(parsed: StoredQuickDeployPreset) {
  if (parsed.cloudInitMode) {
    return {
      cloudInitMode: parsed.cloudInitMode,
      cloudInitExistingRef: parsed.cloudInitExistingRef ?? ''
    }
  }
  const legacyRefs = Array.isArray(parsed.cloudInitRefs)
    ? parsed.cloudInitRefs.filter((ref): ref is string => typeof ref === 'string' && ref.trim().length > 0)
    : []
  if (legacyRefs.length === 0) {
    return { cloudInitMode: 'none' as const, cloudInitExistingRef: '' }
  }
  return { cloudInitMode: 'existing' as const, cloudInitExistingRef: legacyRefs[0] }
}

export function readQuickDeployPreset(): QuickDeployPreset {
  if (typeof window === 'undefined') return initialQuickDeployPreset
  try {
    const raw = localStorage.getItem(QUICK_DEPLOY_STORAGE_KEY)
    if (!raw) return initialQuickDeployPreset
    const parsed = JSON.parse(raw) as StoredQuickDeployPreset
    return {
      ...initialQuickDeployPreset,
      ...parsed,
      name: typeof parsed.name === 'string' ? parsed.name : '',
      count: String(parsed.count ?? '1'),
      ...migrateCloudInitSelection(parsed),
      // Secrets are never persisted; see writeQuickDeployPreset.
      loginUserPassword: '',
      loginUserPasswordTouched: false,
      cloudInitUserData: '',
      sshKeyRefs: Array.isArray(parsed.sshKeyRefs) ? parsed.sshKeyRefs.filter((ref): ref is string => typeof ref === 'string') : []
    }
  } catch {
    return initialQuickDeployPreset
  }
}

// Cloud-Init user-data routinely carries SSH keys and tokens, so it is dropped
// alongside the login password rather than written to localStorage.
export function writeQuickDeployPreset(preset: QuickDeployPreset) {
  if (typeof window === 'undefined') return
  try {
    const { loginUserPassword: _password, cloudInitUserData: _userData, ...safePreset } = preset
    localStorage.setItem(QUICK_DEPLOY_STORAGE_KEY, JSON.stringify({
      ...safePreset,
      count: Math.max(1, Number(preset.count) || 1)
    }))
  } catch {
    // ignore localStorage access errors
  }
}

// The inline Cloud-Init template name is a GOMI resource identifier, so GOMI
// resolves it at deploy time. The user-data body itself is left untouched:
// cloud-init renders its own Jinja on the target from instance-data.
//
// Callers with no concrete hostname (the create and redeploy dialogs, which
// name each VM per iteration) leave the placeholder intact. Substituting an
// empty string would collapse "ci-{{ hostname }}" to "ci-", and the template
// store upserts on name conflict, so that would silently overwrite a shared
// template instead of creating the intended one.
export function renderPresetTemplateName(rawName: string, hostname: string): string {
  if (!hostname) return rawName
  return rawName.replace(/\{\{\s*hostname\s*\}\}/g, hostname)
}

// The Create dialog is a real <form>, so the browser enforces required/min on
// these inputs before submit. Quick Deploy's button sits outside a form, so
// constraint validation never runs and these must be checked explicitly.
// Mirrors the numeric rules in internal/vm/validate.go; the dropdown-backed
// advanced fields cannot express an invalid value from the UI.
export function invalidVMConfigReason(formState: VMConfigForm): string | undefined {
  const positiveFields = [
    { label: 'CPU cores', value: formState.cpuCores },
    { label: 'Memory (MB)', value: formState.memoryMB },
    { label: 'Disk (GB)', value: formState.diskGB }
  ]
  for (const field of positiveFields) {
    const parsed = Number(field.value)
    if (!field.value.trim() || !Number.isFinite(parsed) || parsed <= 0) {
      return `${field.label} must be a positive number`
    }
  }

  const cpuCores = Number(formState.cpuCores)
  const pinning = parseCPUPinning(formState.cpuPinning)
  for (const vcpu of Object.keys(pinning ?? {}).map(Number)) {
    if (vcpu < 0 || vcpu >= cpuCores) {
      return `CPU pinning vcpu ${vcpu} is out of range [0, ${cpuCores})`
    }
  }
  return undefined
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
