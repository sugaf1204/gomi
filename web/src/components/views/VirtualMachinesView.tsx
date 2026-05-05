import { useEffect, useMemo, useRef, useState } from 'react'
import clsx from 'clsx'
import { api } from '../../api'
import { formatDate, phaseClass } from '../../lib/formatters'
import { usePersistentStringState } from '../../hooks/usePersistentStringState'
import { VMConsolePanel } from './VMConsolePanel'
import type { CloudInitTemplate, Hypervisor, OSImage, SSHKey, Subnet, VirtualMachine } from '../../types'
import { SSHAccessFieldset } from './SSHAccessFieldset'

type CloudInitInputMode = 'none' | 'existing' | 'create'

export type VirtualMachinesViewProps = {
  virtualMachines: VirtualMachine[]
  hypervisors: Hypervisor[]
  osImages: OSImage[]
  cloudInits: CloudInitTemplate[]
  sshKeys: SSHKey[]
  subnets: Subnet[]
  dataLoading: boolean
  onVirtualMachineUpsert: (virtualMachine: VirtualMachine) => void
  routeSelectedVM: string
  onRouteSelectedVMChange: (value: string) => void
  onRefresh: () => void | Promise<void>
}

type VMConfigForm = {
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
}

type VMForm = VMConfigForm & {
  name: string
  count: string
}

type VMReinstallForm = VMConfigForm

type UpdateVMConfigForm = (updater: (current: VMConfigForm) => VMConfigForm) => void

type VMPowerAction = 'power-on' | 'power-off'
type VMPrimaryAction = 'console' | 'power-on' | 'power-off' | 'redeploy' | 'migrate' | 'delete'

type VMPowerConfirmState = {
  open: boolean
  action: VMPowerAction
  targets: string[]
  running: boolean
}

type VMDeleteConfirmState = {
  open: boolean
  targets: string[]
  running: boolean
}

type VMBulkRedeployConfirmState = {
  open: boolean
  targets: string[]
  running: boolean
}

type VMMigrateConfirmState = {
  open: boolean
  vmName: string
  targetHypervisor: string
  running: boolean
}

const VM_SELECTION_STORAGE_KEY = 'gomi.virtual-machines.selected'

const initialVMConfigForm: VMConfigForm = {
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
  loginUserPassword: ''
}

const initialForm: VMForm = {
  name: '',
  count: '1',
  ...initialVMConfigForm
}

const initialReinstallForm: VMReinstallForm = { ...initialVMConfigForm }

const initialPowerConfirm: VMPowerConfirmState = {
  open: false,
  action: 'power-on',
  targets: [],
  running: false
}

const initialDeleteConfirm: VMDeleteConfirmState = {
  open: false,
  targets: [],
  running: false
}

const initialBulkRedeployConfirm: VMBulkRedeployConfirmState = {
  open: false,
  targets: [],
  running: false
}

const initialMigrateConfirm: VMMigrateConfirmState = {
  open: false,
  vmName: '',
  targetHypervisor: '',
  running: false
}

function formatCPUPinning(cpuPinning?: Record<number, string>) {
  if (!cpuPinning) return ''
  return Object.entries(cpuPinning)
    .sort(([left], [right]) => Number(left) - Number(right))
    .map(([vcpu, cpuset]) => `${vcpu}:${cpuset}`)
    .join(',')
}

function primaryCloudInitRef(vm: VirtualMachine): string {
  if (vm.lastDeployedCloudInitRef && vm.lastDeployedCloudInitRef.trim().length > 0) {
    return vm.lastDeployedCloudInitRef
  }
  return vm.cloudInitRefs?.[0] ?? vm.cloudInitRef ?? ''
}

function toReinstallForm(vm: VirtualMachine): VMReinstallForm {
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
    loginUserPassword: ''
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

function buildAdvancedOptions(form: Pick<VMConfigForm, 'cpuMode' | 'diskDriver' | 'diskFormat' | 'ioThreads' | 'netMultiqueue' | 'cpuPinning'>): VirtualMachine['advancedOptions'] {
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

export function VirtualMachinesView({
  virtualMachines,
  hypervisors,
  osImages,
  cloudInits,
  sshKeys,
  subnets,
  dataLoading,
  onVirtualMachineUpsert,
  routeSelectedVM,
  onRouteSelectedVMChange,
  onRefresh
}: VirtualMachinesViewProps) {
  const [selected, setSelected] = usePersistentStringState(VM_SELECTION_STORAGE_KEY)
  const [checkedVMs, setCheckedVMs] = useState<Set<string>>(new Set())
  const [formOpen, setFormOpen] = useState(false)
  const [form, setForm] = useState(initialForm)
  const [creating, setCreating] = useState(false)
  const [reinstallOpen, setReinstallOpen] = useState(false)
  const [reinstallForm, setReinstallForm] = useState<VMReinstallForm>(initialReinstallForm)
  const [reinstalling, setReinstalling] = useState(false)
  const [powerConfirm, setPowerConfirm] = useState<VMPowerConfirmState>(initialPowerConfirm)
  const [deleteConfirm, setDeleteConfirm] = useState<VMDeleteConfirmState>(initialDeleteConfirm)
  const [bulkRedeployConfirm, setBulkRedeployConfirm] = useState<VMBulkRedeployConfirmState>(initialBulkRedeployConfirm)
  const [actionsMenuOpen, setActionsMenuOpen] = useState(false)
  const actionsMenuRef = useRef<HTMLDivElement | null>(null)
  const [consoleVM, setConsoleVM] = useState<string | null>(null)
  const [migrateConfirm, setMigrateConfirm] = useState<VMMigrateConfirmState>(initialMigrateConfirm)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [reinstallAdvancedOpen, setReinstallAdvancedOpen] = useState(false)

  const osImageByName = useMemo(() => new Map(osImages.map((img) => [img.name, img])), [osImages])
  const checkedNames = useMemo(
    () => virtualMachines.filter((vm) => checkedVMs.has(vm.name)).map((vm) => vm.name),
    [checkedVMs, virtualMachines]
  )
  const selectedVM = virtualMachines.find((vm) => vm.name === selected) ?? null

  function notifyError(message: string) {
    window.dispatchEvent(new CustomEvent('gomi:toast', { detail: { tone: 'error', message } }))
  }

  function setVMSelection(name: string) {
    setSelected(name)
    onRouteSelectedVMChange(name)
  }

  function formatOSImageReference(ref?: string): string {
    if (!ref) return '-'
    const image = osImageByName.get(ref)
    if (!image) return ref
    const variant = image.variant ? ` ${image.variant}` : ''
    return `${image.name} (${image.osFamily} ${image.osVersion}${variant})`
  }

  function formatCloudInitReferences(vm: VirtualMachine): string {
    const refs = vm.cloudInitRefs?.filter((ref) => ref.trim().length > 0) ?? []
    if (refs.length > 0) return refs.join(', ')
    return vm.cloudInitRef || '-'
  }

  useEffect(() => {
    if (!routeSelectedVM) return
    if (routeSelectedVM !== selected) {
      setSelected(routeSelectedVM)
    }
  }, [routeSelectedVM, selected, setSelected])

  useEffect(() => {
    if (dataLoading) return
    if (!selected) return
    if (routeSelectedVM) return
    if (!virtualMachines.some((vm) => vm.name === selected)) {
      setSelected('')
    }
  }, [dataLoading, routeSelectedVM, selected, virtualMachines, setSelected])

  useEffect(() => {
    if (!formOpen && !reinstallOpen && !powerConfirm.open && !deleteConfirm.open && !bulkRedeployConfirm.open && !migrateConfirm.open) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== 'Escape') return
      if (!powerConfirm.running) setPowerConfirm(initialPowerConfirm)
      if (!reinstalling) setReinstallOpen(false)
      if (!deleteConfirm.running) setDeleteConfirm(initialDeleteConfirm)
      if (!bulkRedeployConfirm.running) setBulkRedeployConfirm(initialBulkRedeployConfirm)
      if (!migrateConfirm.running) setMigrateConfirm(initialMigrateConfirm)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [formOpen, reinstallOpen, reinstalling, powerConfirm.open, powerConfirm.running, deleteConfirm.open, deleteConfirm.running, bulkRedeployConfirm.open, bulkRedeployConfirm.running, migrateConfirm.open, migrateConfirm.running])

  useEffect(() => {
    if (!actionsMenuOpen) return
    function onWindowMouseDown(event: MouseEvent) {
      if (!actionsMenuRef.current) return
      if (event.target instanceof Node && !actionsMenuRef.current.contains(event.target)) {
        setActionsMenuOpen(false)
      }
    }
    function onWindowKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setActionsMenuOpen(false)
      }
    }
    window.addEventListener('mousedown', onWindowMouseDown)
    window.addEventListener('keydown', onWindowKeyDown)
    return () => {
      window.removeEventListener('mousedown', onWindowMouseDown)
      window.removeEventListener('keydown', onWindowKeyDown)
    }
  }, [actionsMenuOpen])

  async function createInlineCloudInitTemplate(templateName: string, userData: string, description: string) {
    const created = await api.createCloudInitTemplate({
      name: templateName,
      description,
      userData
    })
    return created.name
  }

  async function resolveCloudInitRefs(formState: VMConfigForm, description: string) {
    if (formState.cloudInitMode === 'none') return [] as string[]
    if (formState.cloudInitMode === 'existing') {
      const ref = formState.cloudInitExistingRef.trim()
      return ref ? [ref] : []
    }
    const templateName = formState.cloudInitTemplateName.trim()
    const userData = formState.cloudInitUserData.trim()
    if (!templateName || !userData) {
      throw new Error('Cloud-Init inline creation requires both template name and user-data')
    }
    const createdName = await createInlineCloudInitTemplate(
      templateName,
      userData,
      description
    )
    return [createdName]
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    if (!form.osImageRef) {
      notifyError('OS Image is required for PXE deployment')
      return
    }

    setCreating(true)
    try {
      const count = Math.max(1, Math.min(50, Number(form.count) || 1))
      const deployErrors: string[] = []
      const cloudInitRefs = await resolveCloudInitRefs(form, 'Auto-generated from Create Virtual Machine dialog')

      const vmNetwork = (form.bridge || form.staticIP || form.subnetRef)
        ? [{ name: 'default', bridge: form.bridge || undefined, network: form.subnetRef || undefined, ipAddress: (form.ipAssignment === 'static' && form.staticIP) ? form.staticIP : undefined }]
        : undefined

      for (let i = 0; i < count; i++) {
        const vmName = count === 1 ? form.name : `${form.name}-${i + 1}`
        const advancedOptions = buildAdvancedOptions(form)
        const result = await api.createVirtualMachine({
          name: vmName,
          hypervisorRef: form.hypervisorRef || '',
          resources: {
            cpuCores: Number(form.cpuCores) || 2,
            memoryMB: Number(form.memoryMB) || 2048,
            diskGB: Number(form.diskGB) || 20
          },
          osImageRef: form.osImageRef,
          cloudInitRefs: cloudInitRefs.length > 0 ? cloudInitRefs : undefined,
          powerControlMethod: 'libvirt',
          ...(advancedOptions ? { advancedOptions } : {}),
          ...(vmNetwork ? { network: vmNetwork } : {}),
          ...(form.ipAssignment === 'static' ? { ipAssignment: 'static' as const } : {}),
          ...(form.subnetRef ? { subnetRef: form.subnetRef } : {}),
          ...(form.domain.trim() ? { domain: form.domain.trim() } : {}),
          sshKeyRefs: form.sshKeyRefs,
          ...(form.loginUserUsername.trim()
            ? {
                loginUser: {
                  username: form.loginUserUsername.trim(),
                  ...(form.loginUserPassword.trim() ? { password: form.loginUserPassword.trim() } : {})
                }
              }
            : {})
        })
        if (result.phase === 'Error') {
          deployErrors.push(`${vmName}: ${result.lastError || 'deploy failed'}`)
        }
      }

      setForm({ ...initialForm })
      setAdvancedOpen(false)
      setFormOpen(false)
      if (count === 1) {
        setVMSelection(form.name)
      }
      void onRefresh()
      if (deployErrors.length > 0) {
        notifyError(`VM created but deploy failed: ${deployErrors.join('; ')}`)
      }
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to create VM')
    } finally {
      setCreating(false)
    }
  }

  function toggleChecked(name: string) {
    setCheckedVMs((prev) => {
      const next = new Set(prev)
      if (next.has(name)) {
        next.delete(name)
      } else {
        next.add(name)
      }
      return next
    })
  }

  function toggleAllChecked() {
    if (checkedVMs.size === virtualMachines.length) {
      setCheckedVMs(new Set())
    } else {
      setCheckedVMs(new Set(virtualMachines.map((vm) => vm.name)))
    }
  }

  function openDeleteDialog(targets: string[]) {
    setDeleteConfirm({
      open: true,
      targets,
      running: false
    })
  }

  async function handleDeleteConfirm() {
    if (deleteConfirm.targets.length === 0) return
    const targets = [...deleteConfirm.targets]
    setDeleteConfirm(initialDeleteConfirm)
    try {
      for (const target of targets) {
        await api.deleteVirtualMachine(target)
      }
      if (targets.includes(selected)) {
        setSelected('')
        onRouteSelectedVMChange('')
      }
      setCheckedVMs((prev) => {
        const next = new Set(prev)
        for (const t of targets) next.delete(t)
        return next
      })
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to delete VM')
    }
  }

  async function handleBulkRedeployConfirm() {
    if (bulkRedeployConfirm.targets.length === 0) return
    const targets = [...bulkRedeployConfirm.targets]
    setBulkRedeployConfirm(initialBulkRedeployConfirm)
    try {
      for (const target of targets) {
        await api.vmRedeploy(target)
      }
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to redeploy VM')
    }
  }

  function openPowerConfirm(action: VMPowerAction, targets: string[]) {
    setPowerConfirm({
      open: true,
      action,
      targets,
      running: false
    })
  }

  async function handlePowerConfirm() {
    if (powerConfirm.targets.length === 0) return
    const action = powerConfirm.action
    const targets = [...powerConfirm.targets]
    setPowerConfirm(initialPowerConfirm)
    try {
      for (const target of targets) {
        if (action === 'power-on') {
          await api.vmPowerOn(target)
        } else {
          await api.vmPowerOff(target)
        }
      }
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : `Failed to ${action === 'power-on' ? 'power on' : 'power off'} VM`)
    }
  }

  function openReinstallDialog(target: VirtualMachine) {
    setReinstallForm(toReinstallForm(target))
    setReinstallAdvancedOpen(Boolean(target.advancedOptions))
    setReinstallOpen(true)
  }

  async function handleRedeploy() {
    if (!selectedVM) return
    if (!reinstallForm.osImageRef) {
      notifyError('OS Image is required for redeploy')
      return
    }

    const cloudInitRefs = await resolveCloudInitRefs(reinstallForm, 'Auto-generated from Redeploy Virtual Machine dialog')
    const vmNetwork = (reinstallForm.bridge || reinstallForm.staticIP || reinstallForm.subnetRef)
      ? [{ name: 'default', bridge: reinstallForm.bridge || undefined, network: reinstallForm.subnetRef || undefined, ipAddress: reinstallForm.ipAssignment === 'static' && reinstallForm.staticIP ? reinstallForm.staticIP : undefined }]
      : undefined
    const advancedOptions = buildAdvancedOptions(reinstallForm) ?? {}
    const redeployPayload: Parameters<typeof api.vmRedeploy>[1] = {
      hypervisorRef: reinstallForm.hypervisorRef || '',
      resources: {
        cpuCores: Number(reinstallForm.cpuCores) || 2,
        memoryMB: Number(reinstallForm.memoryMB) || 2048,
        diskGB: Number(reinstallForm.diskGB) || 20
      },
      osImageRef: reinstallForm.osImageRef,
      cloudInitRefs,
      ...(vmNetwork ? { network: vmNetwork } : {}),
      ...(reinstallForm.subnetRef ? { subnetRef: reinstallForm.subnetRef } : { subnetRef: '' }),
      domain: reinstallForm.domain.trim() || undefined,
      advancedOptions,
      ipAssignment: reinstallForm.ipAssignment,
      ...(reinstallForm.ipAssignment === 'static' && reinstallForm.staticIP ? { ip: reinstallForm.staticIP } : { ip: '' }),
      sshKeyRefs: reinstallForm.sshKeyRefs,
      ...(reinstallForm.loginUserUsername.trim()
        ? {
            loginUser: {
              username: reinstallForm.loginUserUsername.trim(),
              ...(reinstallForm.loginUserPassword.trim() ? { password: reinstallForm.loginUserPassword.trim() } : {})
            }
          }
        : {})
    }

    setReinstalling(true)
    try {
      const updated = await api.vmRedeploy(selectedVM.name, redeployPayload)
      onVirtualMachineUpsert(updated)
      setReinstallOpen(false)
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to redeploy VM')
    } finally {
      setReinstalling(false)
    }
  }

  async function handleMigrateConfirm() {
    if (!migrateConfirm.vmName) return
    setMigrateConfirm(prev => ({ ...prev, running: true }))
    try {
      const payload: { targetHypervisor?: string } = {}
      if (migrateConfirm.targetHypervisor) {
        payload.targetHypervisor = migrateConfirm.targetHypervisor
      }
      await api.vmMigrate(migrateConfirm.vmName, payload)
      setMigrateConfirm(initialMigrateConfirm)
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to migrate VM')
      setMigrateConfirm(prev => ({ ...prev, running: false }))
    }
  }

  function runPrimaryAction(action: VMPrimaryAction) {
    const targets = checkedNames.length > 0 ? checkedNames : (selectedVM ? [selectedVM.name] : [])
    if (targets.length === 0) return

    switch (action) {
      case 'console':
        if (selectedVM && (selectedVM.phase === 'Running' || selectedVM.phase === 'Provisioning')) {
          setConsoleVM(selectedVM.name)
        }
        break
      case 'power-on':
        openPowerConfirm('power-on', targets)
        break
      case 'power-off':
        openPowerConfirm('power-off', targets)
        break
      case 'redeploy':
        if (checkedNames.length > 0) {
          setBulkRedeployConfirm({ open: true, targets, running: false })
        } else if (selectedVM) {
          openReinstallDialog(selectedVM)
        }
        break
      case 'migrate':
        if (selectedVM && selectedVM.phase === 'Running') {
          setMigrateConfirm({
            open: true,
            vmName: selectedVM.name,
            targetHypervisor: '',
            running: false
          })
        }
        break
      case 'delete':
        openDeleteDialog(targets)
        break
      default:
        break
    }
  }

  const updateCreateConfigForm: UpdateVMConfigForm = (updater) => {
    setForm((current) => ({
      ...current,
      ...updater(current)
    }))
  }

  const updateReinstallConfigForm: UpdateVMConfigForm = (updater) => {
    setReinstallForm((current) => updater(current))
  }

  function bridgePlaceholder(formState: VMConfigForm): string {
    const hypervisor = hypervisors.find((item) => item.name === formState.hypervisorRef)
    if (hypervisor?.bridgeName) {
      return hypervisor.bridgeName
    }
    if (formState.subnetRef) {
      return subnets.find((item) => item.name === formState.subnetRef)?.spec.pxeInterface || 'virbr0'
    }
    return 'virbr0'
  }

  function renderVMSpecFields(
    formState: VMConfigForm,
    updateForm: UpdateVMConfigForm,
    advancedExpanded: boolean,
    setAdvancedExpanded: (value: boolean | ((current: boolean) => boolean)) => void,
    radioNamePrefix: string
  ) {
    return (
      <>
        <label className="text-[0.84rem]">
          Hypervisor
          <select value={formState.hypervisorRef} onChange={(e) => {
            const hvName = e.target.value
            const hv = hypervisors.find((item) => item.name === hvName)
            updateForm((current) => ({ ...current, hypervisorRef: hvName, bridge: hv?.bridgeName || current.bridge }))
          }}>
            <option value="">Auto (lowest usage)</option>
            {hypervisors.map((hv) => (
              <option key={hv.name} value={hv.name}>{hv.name} ({hv.connection.host}){hv.bridgeName ? ` [${hv.bridgeName}]` : ''}</option>
            ))}
          </select>
        </label>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-[0.45rem]">
          <label className="text-[0.84rem] min-w-0">
            CPU Cores
            <input type="number" required min="1" value={formState.cpuCores} onChange={(e) => updateForm((current) => ({ ...current, cpuCores: e.target.value }))} />
          </label>
          <label className="text-[0.84rem] min-w-0">
            Memory (MB)
            <input type="number" required min="256" value={formState.memoryMB} onChange={(e) => updateForm((current) => ({ ...current, memoryMB: e.target.value }))} />
          </label>
          <label className="text-[0.84rem] min-w-0">
            Disk (GB)
            <input type="number" required min="1" value={formState.diskGB} onChange={(e) => updateForm((current) => ({ ...current, diskGB: e.target.value }))} />
          </label>
        </div>
        <label className="text-[0.84rem]">
          OS Image
          <select required value={formState.osImageRef} onChange={(e) => updateForm((current) => ({ ...current, osImageRef: e.target.value }))}>
            <option value="">Select...</option>
            {osImages.map((img) => (
              <option key={img.name} value={img.name}>{img.name} ({img.osFamily} {img.osVersion}{img.variant ? ` ${img.variant}` : ''})</option>
            ))}
          </select>
        </label>
        <label className="text-[0.84rem]">
          Cloud-Init
          <select value={formState.cloudInitMode} onChange={(e) => updateForm((current) => ({ ...current, cloudInitMode: e.target.value as CloudInitInputMode }))}>
            <option value="none">None</option>
            <option value="existing">Use existing template</option>
            <option value="create">Create template inline</option>
          </select>
        </label>
        {formState.cloudInitMode === 'existing' && (
          <label className="text-[0.84rem]">
            Existing Cloud-Init Template
            <select value={formState.cloudInitExistingRef} onChange={(e) => updateForm((current) => ({ ...current, cloudInitExistingRef: e.target.value }))}>
              <option value="">None</option>
              {cloudInits.map((ci) => (
                <option key={ci.name} value={ci.name}>{ci.name}</option>
              ))}
            </select>
          </label>
        )}
        {formState.cloudInitMode === 'existing' && cloudInits.length === 0 && (
          <p className="m-0 text-[0.78rem] text-ink-soft">No Cloud-Init templates available. Switch mode to create one inline.</p>
        )}
        {formState.cloudInitMode === 'create' && (
          <>
            <label className="text-[0.84rem]">
              Cloud-Init Template Name
              <input
                value={formState.cloudInitTemplateName}
                onChange={(e) => updateForm((current) => ({ ...current, cloudInitTemplateName: e.target.value }))}
                placeholder="e.g. vm-inline-template"
              />
            </label>
            <label className="text-[0.84rem]">
              Cloud-Init User Data
              <textarea
                className="min-h-[140px] font-mono text-[0.78rem]"
                value={formState.cloudInitUserData}
                onChange={(e) => updateForm((current) => ({ ...current, cloudInitUserData: e.target.value }))}
                placeholder="#cloud-config\nusers:\n  - default"
              />
            </label>
          </>
        )}
        <SSHAccessFieldset
          sshKeys={sshKeys}
          value={formState}
          onChange={(updater) => updateForm((current) => ({ ...current, ...updater(current) }))}
          onRefresh={onRefresh}
        />
        <fieldset className="border border-line rounded p-0 m-0">
          <legend className="text-[0.84rem] font-medium px-[0.4rem] ml-[0.3rem]">Network</legend>
          <div className="grid gap-[0.45rem] p-[0.7rem]">
            <label className="text-[0.84rem]">
              Subnet
              <select value={formState.subnetRef} onChange={(e) => {
                const subnetName = e.target.value
                const subnet = subnets.find((item) => item.name === subnetName)
                updateForm((current) => ({
                  ...current,
                  subnetRef: subnetName,
                  bridge: subnet?.spec.pxeInterface || ''
                }))
              }}>
                <option value="">None (use default)</option>
                {subnets.map((subnet) => (
                  <option key={subnet.name} value={subnet.name}>{subnet.name} ({subnet.spec.cidr})</option>
                ))}
              </select>
            </label>
            <div className="flex items-center gap-[0.8rem] text-[0.84rem]">
              <span>IP Assignment:</span>
              <label className="flex items-center gap-1 cursor-pointer">
                <input type="radio" name={`${radioNamePrefix}-ip-assignment`} checked={formState.ipAssignment === 'dhcp'} onChange={() => updateForm((current) => ({ ...current, ipAssignment: 'dhcp' }))} />
                DHCP
              </label>
              <label className="flex items-center gap-1 cursor-pointer">
                <input type="radio" name={`${radioNamePrefix}-ip-assignment`} checked={formState.ipAssignment === 'static'} onChange={() => updateForm((current) => ({ ...current, ipAssignment: 'static' }))} />
                Static
              </label>
            </div>
            {formState.ipAssignment === 'static' && (
              <label className="text-[0.84rem]">
                Static IP
                <input value={formState.staticIP} onChange={(e) => updateForm((current) => ({ ...current, staticIP: e.target.value }))} placeholder="e.g. 192.168.1.100" />
              </label>
            )}
            <label className="text-[0.84rem]">
              Bridge
              <input value={formState.bridge} onChange={(e) => updateForm((current) => ({ ...current, bridge: e.target.value }))} placeholder={bridgePlaceholder(formState)} />
            </label>
            <label className="text-[0.84rem]">
              Domain
              <input value={formState.domain} onChange={(e) => updateForm((current) => ({ ...current, domain: e.target.value }))} placeholder="e.g. example.local" />
            </label>
          </div>
        </fieldset>
        <div className="border border-line rounded">
          <button
            type="button"
            className="w-full text-left border-0 shadow-none rounded-none px-[0.7rem] py-[0.5rem] text-[0.84rem] font-medium bg-[#f9f7f4] hover:bg-[#f3efe8]"
            onClick={() => setAdvancedExpanded((current) => !current)}
          >
            {advancedExpanded ? '▾' : '▸'} Advanced Options
          </button>
          {advancedExpanded && (
            <div className="grid gap-[0.45rem] p-[0.7rem] border-t border-line">
              <label className="text-[0.84rem] min-w-0">
                CPU Mode
                <select value={formState.cpuMode} onChange={(e) => updateForm((current) => ({ ...current, cpuMode: e.target.value as VMConfigForm['cpuMode'] }))}>
                  <option value="">Default</option>
                  <option value="host-passthrough">host-passthrough</option>
                  <option value="host-model">host-model</option>
                  <option value="maximum">maximum</option>
                </select>
              </label>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
                <label className="text-[0.84rem] min-w-0">
                  Disk Driver
                  <select value={formState.diskDriver} onChange={(e) => updateForm((current) => ({ ...current, diskDriver: e.target.value }))}>
                    <option value="">Default (virtio)</option>
                    <option value="virtio">virtio</option>
                    <option value="scsi">scsi (virtio-scsi)</option>
                  </select>
                </label>
                <label className="text-[0.84rem] min-w-0">
                  Disk Format
                  <select value={formState.diskFormat} onChange={(e) => updateForm((current) => ({ ...current, diskFormat: e.target.value }))}>
                    <option value="">Default (qcow2)</option>
                    <option value="qcow2">qcow2</option>
                  </select>
                </label>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
                <label className="text-[0.84rem] min-w-0">
                  IO Threads
                  <input type="number" min="0" max="16" value={formState.ioThreads} onChange={(e) => updateForm((current) => ({ ...current, ioThreads: e.target.value }))} placeholder="0 (disabled)" />
                </label>
                <label className="text-[0.84rem] min-w-0">
                  Net Multiqueue
                  <input type="number" min="0" max="16" value={formState.netMultiqueue} onChange={(e) => updateForm((current) => ({ ...current, netMultiqueue: e.target.value }))} placeholder="0 (disabled)" />
                </label>
              </div>
              <label className="text-[0.84rem]">
                CPU Pinning
                <input value={formState.cpuPinning} onChange={(e) => updateForm((current) => ({ ...current, cpuPinning: e.target.value }))} placeholder="e.g. 0:0,1:2,2:4 (vcpu:cpuset)" />
              </label>
            </div>
          )}
        </div>
      </>
    )
  }

  return (
    <>
      {formOpen && (
        <div
          className="fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4"
          role="dialog"
          aria-modal="true"
          onClick={(e) => { if (e.target === e.currentTarget && !creating) { setFormOpen(false) } }}
        >
          <div className="w-[min(760px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">Create Virtual Machine</h3>
              <button
                aria-label="Close"
                className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                disabled={creating}
                onClick={() => { setFormOpen(false) }}
              >×</button>
            </div>
            <form className="grid gap-[0.55rem]" onSubmit={(e) => void handleCreate(e)}>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.55rem]">
                <label className="text-[0.84rem] min-w-0">
                  Name
                  <input required value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} placeholder="e.g. vm-web-01" />
                </label>
                <label className="text-[0.84rem] min-w-0">
                  Count
                  <input type="number" min="1" max="50" value={form.count} onChange={(e) => setForm((f) => ({ ...f, count: e.target.value }))} />
                </label>
              </div>
              {Number(form.count) > 1 && form.name && (
                <p className="m-0 text-ink-soft text-[0.82rem]">
                  Will create: {Array.from({ length: Math.min(Number(form.count) || 1, 5) }, (_, i) => `${form.name}-${i + 1}`).join(', ')}
                  {Number(form.count) > 5 && `, ... (${form.count} total)`}
                </p>
              )}
              {renderVMSpecFields(form, updateCreateConfigForm, advancedOpen, setAdvancedOpen, 'vm-create')}
              <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
                <button type="button" onClick={() => { setFormOpen(false) }} disabled={creating}>Cancel</button>
                <button
                  type="submit"
                  disabled={
                    creating
                    || !form.name.trim()
                    || !form.osImageRef
                    || (form.ipAssignment === 'static' && !form.staticIP.trim())
                    || (form.cloudInitMode === 'create' && (!form.cloudInitTemplateName.trim() || !form.cloudInitUserData.trim()))
                  }
                  className="bg-brand border-brand-strong text-white"
                >
                  {creating ? 'Creating...' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {reinstallOpen && selectedVM && (
        <div
          className="fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4"
          role="dialog"
          aria-modal="true"
          onClick={(e) => { if (e.target === e.currentTarget && !reinstalling) { setReinstallOpen(false) } }}
        >
          <div className="w-[min(760px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">Redeploy VM: {selectedVM.name}</h3>
              <button
                aria-label="Close"
                className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                disabled={reinstalling}
                onClick={() => { setReinstallOpen(false) }}
              >×</button>
            </div>
            <p className="m-0 text-ink-soft text-[0.84rem]">Current VM spec is pre-filled. You can redeploy as-is or update values before execution.</p>
            <div className="grid gap-[0.55rem]">
              {renderVMSpecFields(reinstallForm, updateReinstallConfigForm, reinstallAdvancedOpen, setReinstallAdvancedOpen, 'vm-redeploy')}
              <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
                <button type="button" onClick={() => { setReinstallOpen(false) }}>Cancel</button>
                <button
                  type="button"
                  disabled={
                    reinstalling
                    || !reinstallForm.osImageRef
                    || (reinstallForm.ipAssignment === 'static' && !reinstallForm.staticIP.trim())
                    || (reinstallForm.cloudInitMode === 'create' && (!reinstallForm.cloudInitTemplateName.trim() || !reinstallForm.cloudInitUserData.trim()))
                  }
                  className="bg-brand border-brand-strong text-white"
                  onClick={() => void handleRedeploy()}
                >
                  {reinstalling ? 'Redeploying...' : 'Redeploy'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {deleteConfirm.open && (
        <div
          className="fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4"
          role="dialog"
          aria-modal="true"
          onClick={(e) => {
            if (e.target === e.currentTarget && !deleteConfirm.running) {
              setDeleteConfirm(initialDeleteConfirm)
            }
          }}
        >
          <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem] text-[#9b2d2d]">Delete Virtual Machine{deleteConfirm.targets.length > 1 ? 's' : ''}</h3>
              <button
                aria-label="Close"
                className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                disabled={deleteConfirm.running}
                onClick={() => { setDeleteConfirm(initialDeleteConfirm) }}
              >×</button>
            </div>
            <p className="m-0 text-ink-soft text-[0.84rem]">This action permanently removes runtime resources and cannot be undone.</p>
            <div className="max-h-[180px] overflow-auto border border-line p-[0.55rem] bg-[#f9f7f4]">
              <ul className="m-0 pl-[1.1rem]">
                {deleteConfirm.targets.map((target) => (
                  <li key={target}><code>{target}</code></li>
                ))}
              </ul>
            </div>
            <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
              <button type="button" onClick={() => setDeleteConfirm(initialDeleteConfirm)} disabled={deleteConfirm.running}>Cancel</button>
              <button
                type="button"
                disabled={deleteConfirm.running}
                className="bg-[#d86b6b] border-[#be5252] text-white"
                onClick={() => void handleDeleteConfirm()}
              >
                {deleteConfirm.running ? 'Deleting...' : `Delete (${deleteConfirm.targets.length})`}
              </button>
            </div>
          </div>
        </div>
      )}

      {bulkRedeployConfirm.open && (
        <div
          className="fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4"
          role="dialog"
          aria-modal="true"
          onClick={(e) => {
            if (e.target === e.currentTarget && !bulkRedeployConfirm.running) {
              setBulkRedeployConfirm(initialBulkRedeployConfirm)
            }
          }}
        >
          <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
            <h3 className="text-[1.2rem]">Confirm Bulk Redeploy</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">
              The following VMs will be redeployed with their current specs ({bulkRedeployConfirm.targets.length}):
            </p>
            <div className="max-h-[180px] overflow-auto border border-line p-[0.55rem] bg-[#f9f7f4]">
              <ul className="m-0 pl-[1.1rem]">
                {bulkRedeployConfirm.targets.map((target) => (
                  <li key={target}><code>{target}</code></li>
                ))}
              </ul>
            </div>
            <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
              <button type="button" onClick={() => setBulkRedeployConfirm(initialBulkRedeployConfirm)} disabled={bulkRedeployConfirm.running}>Cancel</button>
              <button
                type="button"
                disabled={bulkRedeployConfirm.running}
                className="bg-brand border-brand-strong text-white"
                onClick={() => void handleBulkRedeployConfirm()}
              >
                {bulkRedeployConfirm.running ? 'Redeploying...' : `Redeploy (${bulkRedeployConfirm.targets.length})`}
              </button>
            </div>
          </div>
        </div>
      )}

      {powerConfirm.open && (
        <div
          className="fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4"
          role="dialog"
          aria-modal="true"
          onClick={(e) => {
            if (e.target === e.currentTarget && !powerConfirm.running) {
              setPowerConfirm(initialPowerConfirm)
            }
          }}
        >
          <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
            <h3 className="text-[1.2rem]">{powerConfirm.action === 'power-on' ? 'Confirm Power On' : 'Confirm Power Off'}</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">
              Target virtual machines ({powerConfirm.targets.length}):
            </p>
            <div className="max-h-[180px] overflow-auto border border-line p-[0.55rem] bg-[#f9f7f4]">
              <ul className="m-0 pl-[1.1rem]">
                {powerConfirm.targets.map((target) => (
                  <li key={target}><code>{target}</code></li>
                ))}
              </ul>
            </div>
            <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
              <button type="button" onClick={() => setPowerConfirm(initialPowerConfirm)} disabled={powerConfirm.running}>Cancel</button>
              <button
                type="button"
                disabled={powerConfirm.running}
                className={clsx(
                  'text-white',
                  powerConfirm.action === 'power-on' ? 'bg-brand border-brand-strong' : 'bg-[#d86b6b] border-[#be5252]'
                )}
                onClick={() => void handlePowerConfirm()}
              >
                {powerConfirm.running
                  ? (powerConfirm.action === 'power-on' ? 'Powering On...' : 'Powering Off...')
                  : (powerConfirm.action === 'power-on' ? 'Power On' : 'Power Off')}
              </button>
            </div>
          </div>
        </div>
      )}

      {migrateConfirm.open && (() => {
        const migrateVM = virtualMachines.find((v) => v.name === migrateConfirm.vmName)
        const currentHV = migrateVM?.hypervisorName || migrateVM?.hypervisorRef || ''
        const otherHVs = hypervisors.filter((hv) => hv.name !== currentHV)
        return (
          <div
            className="fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4"
            role="dialog"
            aria-modal="true"
            onClick={(e) => {
              if (e.target === e.currentTarget && !migrateConfirm.running) {
                setMigrateConfirm(initialMigrateConfirm)
              }
            }}
          >
            <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
              <div className="flex justify-between items-center">
                <h3 className="text-[1.2rem]">Migrate VM: {migrateConfirm.vmName}</h3>
                <button
                  aria-label="Close"
                  className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                  disabled={migrateConfirm.running}
                  onClick={() => setMigrateConfirm(initialMigrateConfirm)}
                >×</button>
              </div>
              <p className="m-0 text-ink-soft text-[0.84rem]">
                Live-migrate the running VM to another hypervisor. The VM will remain online during migration.
              </p>
              <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                <dt className="text-ink-soft text-[0.84rem]">Source Hypervisor</dt>
                <dd className="m-0 font-medium">{currentHV || 'Unknown'}</dd>
              </dl>
              <label className="text-[0.84rem]">
                Target Hypervisor
                <select
                  value={migrateConfirm.targetHypervisor}
                  onChange={(e) => setMigrateConfirm((prev) => ({ ...prev, targetHypervisor: e.target.value }))}
                  disabled={migrateConfirm.running}
                >
                  <option value="">Auto (best available)</option>
                  {otherHVs.map((hv) => (
                    <option key={hv.name} value={hv.name}>
                      {hv.name} ({hv.connection.host})
                    </option>
                  ))}
                </select>
              </label>
              <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
                <button type="button" onClick={() => setMigrateConfirm(initialMigrateConfirm)} disabled={migrateConfirm.running}>Cancel</button>
                <button
                  type="button"
                  disabled={migrateConfirm.running}
                  className="bg-brand border-brand-strong text-white"
                  onClick={() => void handleMigrateConfirm()}
                >
                  {migrateConfirm.running ? 'Migrating...' : 'Migrate'}
                </button>
              </div>
            </div>
          </div>
        )
      })()}

      <section className="min-h-0 grid grid-cols-1 md:grid-cols-[310px_minmax(0,1fr)] gap-[0.9rem]">
        <div className="min-h-0 grid grid-rows-[auto_minmax(0,1fr)] gap-[0.6rem]">
          <div className="grid gap-[0.45rem]">
            <div className="flex justify-between items-center gap-2">
              <h2 className="text-[1.4rem]">Virtual Machines</h2>
              <button
                className="bg-brand border-brand-strong text-white py-[0.35rem] px-[0.55rem] text-[0.82rem]"
                onClick={() => {
                  setForm({ ...initialForm })
                  setAdvancedOpen(false)
                  setFormOpen(true)
                }}
              >
                Create
              </button>
            </div>
          </div>

          <div className="overflow-auto border-t border-line pr-[0.1rem]">
            {dataLoading && virtualMachines.length === 0 && (
              <div className="grid place-items-center py-[2.4rem] text-center text-ink-soft gap-[0.55rem]">
                <div className="loading-spinner" aria-hidden="true" />
                <p className="m-0 text-[0.84rem]">Loading virtual machines...</p>
              </div>
            )}
            {virtualMachines.length > 0 && (
              <div className="flex items-center gap-[0.45rem] py-[0.35rem] pl-[0.55rem] border-b border-line bg-[#f9f7f4]">
                <input
                  type="checkbox"
                  className="w-[0.95rem] h-[0.95rem] m-0 shrink-0 accent-[#2b7a78] cursor-pointer"
                  checked={checkedVMs.size === virtualMachines.length && virtualMachines.length > 0}
                  onChange={toggleAllChecked}
                />
                <span className="text-[0.78rem] text-ink-soft">Select All</span>
              </div>
            )}
            {virtualMachines.map((vm) => (
              <div
                key={vm.name}
                className={clsx(
                  'flex items-start border-0 border-b border-line border-l-[3px] border-l-transparent',
                  selected === vm.name && '!border-l-brand bg-[rgba(43,122,120,0.06)]'
                )}
              >
                <div className="flex items-center pl-[0.55rem] pt-[0.72rem] shrink-0">
                  <input
                    type="checkbox"
                    className="w-[0.95rem] h-[0.95rem] m-0 accent-[#2b7a78] cursor-pointer"
                    checked={checkedVMs.has(vm.name)}
                    onChange={() => toggleChecked(vm.name)}
                  />
                </div>
                <button
                  className="text-left flex-1 min-w-0 border-0 bg-transparent flex justify-between items-start gap-[0.75rem] py-[0.62rem] pl-[0.45rem] pr-[0.1rem] shadow-none hover:transform-none!"
                  onClick={() => setVMSelection(vm.name)}
                >
                  <div className="min-w-0">
                    <p className="m-0 font-ui font-medium tracking-normal truncate">{vm.name}</p>
                    <p className="m-0 text-ink-soft text-[0.82rem]">{vm.hypervisorRef || 'Auto-placed'} - {vm.resources.cpuCores}CPU - {vm.resources.memoryMB}MB</p>
                    {vm.phase === 'Error' && vm.lastError && (
                      <p className="m-0 text-[#7f2727] text-[0.76rem] mt-[0.15rem] leading-tight">{vm.lastError}</p>
                    )}
                  </div>
                  <span className={clsx(phaseClass(vm.phase), 'shrink-0')}>{vm.phase}</span>
                </button>
              </div>
            ))}
            {!dataLoading && virtualMachines.length === 0 && <p className="m-0 text-ink-soft">No virtual machines found</p>}
          </div>
        </div>

        <div className="min-h-0 grid content-start gap-[0.85rem]">
          {!selectedVM && checkedNames.length === 0 && (
            <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.45rem]">
              <h2>Select a virtual machine</h2>
              <p>Choose a VM from the list to view details and actions.</p>
            </section>
          )}

          {(selectedVM || checkedNames.length > 0) && (
            <>
              <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] flex justify-between items-start gap-4">
                <div>
                  <p className="m-0 font-ui font-medium text-[0.72rem] uppercase tracking-[0.08em] text-ink-soft">Virtual Machine</p>
                  {checkedNames.length > 0 ? (
                    <h2 className="mt-[0.22rem] text-[1.8rem]">{checkedNames.length} selected</h2>
                  ) : selectedVM && (
                    <>
                      <h2 className="mt-[0.22rem] text-[1.8rem]">{selectedVM.name}</h2>
                      <p className="m-0 text-ink-soft">
                        {selectedVM.hypervisorRef
                          ? selectedVM.hypervisorRef
                          : selectedVM.hypervisorName
                            ? `Auto-placed on: ${selectedVM.hypervisorName}`
                            : 'Auto-placement (pending)'}
                        {' - '}PXE
                      </p>
                    </>
                  )}
                </div>
                <div className="flex flex-col items-end gap-[0.55rem]">
                  {checkedNames.length === 0 && selectedVM && (
                    <span className={phaseClass(selectedVM.phase)}>{selectedVM.phase}</span>
                  )}
                  <div className="flex justify-end items-center flex-wrap gap-[0.35rem]" ref={actionsMenuRef}>
                    <div className="relative">
                      <button
                        className="py-[0.45rem] px-[0.72rem]"
                        disabled={checkedNames.length === 0 && !selectedVM}
                        onClick={() => setActionsMenuOpen((current) => !current)}
                      >
                        Actions
                      </button>
                      {actionsMenuOpen && (
                        <div className="absolute right-0 mt-1 min-w-[180px] bg-white border border-line shadow-[0_10px_24px_rgba(52,43,34,0.16)] z-10">
                          {([
                            { value: 'console', label: 'Console' },
                            { value: 'power-on', label: 'Power On' },
                            { value: 'power-off', label: 'Power Off' },
                            { value: 'redeploy', label: 'Redeploy' },
                            { value: 'migrate', label: 'Migrate' },
                            { value: 'delete', label: 'Delete' }
                          ] as Array<{ value: VMPrimaryAction, label: string }>).map((item) => (
                            <button
                              key={item.value}
                              className={clsx(
                                'w-full text-left border-0 shadow-none rounded-none px-[0.7rem] py-[0.5rem]',
                                item.value === 'power-off' || item.value === 'delete'
                                  ? 'text-[#9b2d2d] hover:bg-[#fff3f2]'
                                  : 'text-ink hover:bg-[#f7f3ed]'
                              )}
                              onClick={() => {
                                setActionsMenuOpen(false)
                                runPrimaryAction(item.value)
                              }}
                            >
                              {item.label}
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              </section>
            </>
          )}

          {consoleVM && (() => {
            const cvmObj = virtualMachines.find((v) => v.name === consoleVM)
            return cvmObj ? (
              <VMConsolePanel vm={cvmObj} onClose={() => setConsoleVM(null)} />
            ) : null
          })()}

          {selectedVM && (
            <>
              <section className="grid grid-cols-1 lg:grid-cols-2 gap-[0.85rem]">
                <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                  <h3 className="mb-[0.58rem] text-[1.05rem]">Resources</h3>
                  <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                    <dt className="text-ink-soft text-[0.84rem]">CPU Cores</dt><dd className="m-0">{selectedVM.resources.cpuCores}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Memory (MB)</dt><dd className="m-0">{selectedVM.resources.memoryMB}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Disk (GB)</dt><dd className="m-0">{selectedVM.resources.diskGB}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Power Control</dt><dd className="m-0">{selectedVM.powerControlMethod}</dd>
                  </dl>
                </article>

                <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                  <h3 className="mb-[0.58rem] text-[1.05rem]">Status</h3>
                  <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                    <dt className="text-ink-soft text-[0.84rem]">Phase</dt>
                    <dd className="m-0">
                      <span className={phaseClass(selectedVM.phase)}>{selectedVM.phase}</span>
                    </dd>
                    <dt className="text-ink-soft text-[0.84rem]">Hypervisor</dt><dd className="m-0">{selectedVM.hypervisorName || '-'}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Domain</dt><dd className="m-0"><code className="text-[0.82rem]">{selectedVM.libvirtDomain || '-'}</code></dd>
                    <dt className="text-ink-soft text-[0.84rem]">IP Addresses</dt>
                    <dd className="m-0 flex items-center gap-[0.4rem] flex-wrap">
                      <span>{selectedVM.ipAddresses?.join(', ') || '-'}</span>
                      <span className={clsx(
                        'text-[0.68rem] font-medium px-[0.35rem] py-[0.05rem] rounded-sm border',
                        selectedVM.ipAssignment === 'static'
                          ? 'bg-[#e8f4f3] text-[#1a6360] border-[#b8dbd9]'
                          : 'bg-[#f3f0ea] text-ink-soft border-[#e0dbd2]'
                      )}>
                        {selectedVM.ipAssignment === 'static' ? 'Static' : 'DHCP'}
                      </span>
                    </dd>
                    <dt className="text-ink-soft text-[0.84rem]">MAC Addresses</dt>
                    <dd className="m-0">
                      {selectedVM.networkInterfaces
                        ?.map((nic) => nic.mac)
                        .filter((mac): mac is string => Boolean(mac))
                        .join(', ') || '-'}
                    </dd>
                    <dt className="text-ink-soft text-[0.84rem]">Created On Host</dt><dd className="m-0">{selectedVM.createdOnHost || '-'}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Last Power Action</dt><dd className="m-0">{selectedVM.lastPowerAction || '-'}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Last Error</dt><dd className="m-0 text-error">{selectedVM.lastError || '-'}</dd>
                  </dl>
                </article>
              </section>

              <section className="grid grid-cols-1 lg:grid-cols-2 gap-[0.85rem]">
                <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                  <h3 className="mb-[0.58rem] text-[1.05rem]">Provisioning</h3>
                  <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                    <dt className="text-ink-soft text-[0.84rem]">Active</dt><dd className="m-0">{selectedVM.provisioning?.active ? 'Yes' : 'No'}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Started</dt><dd className="m-0">{formatDate(selectedVM.provisioning?.startedAt)}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Deadline</dt><dd className="m-0">{formatDate(selectedVM.provisioning?.deadlineAt)}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Completed</dt><dd className="m-0">{formatDate(selectedVM.provisioning?.completedAt)}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Completion Source</dt><dd className="m-0">{selectedVM.provisioning?.completionSource || '-'}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Last Signal</dt><dd className="m-0">{formatDate(selectedVM.provisioning?.lastSignalAt)}</dd>
                  </dl>
                </article>

                <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                  <h3 className="mb-[0.58rem] text-[1.05rem]">References</h3>
                  <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                    <dt className="text-ink-soft text-[0.84rem]">Hypervisor</dt><dd className="m-0">{selectedVM.hypervisorRef || 'Auto (scheduled by server)'}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Subnet</dt><dd className="m-0">{selectedVM.subnetRef || '-'}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">DNS Domain</dt><dd className="m-0">{selectedVM.domain || '-'}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">OS Image</dt><dd className="m-0">{formatOSImageReference(selectedVM.osImageRef)}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Cloud-Init</dt><dd className="m-0">{formatCloudInitReferences(selectedVM)}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Install Config</dt><dd className="m-0">{selectedVM.installCfg?.type || '-'}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Last Deployed Cloud-Init</dt><dd className="m-0">{selectedVM.lastDeployedCloudInitRef || '-'}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Created</dt><dd className="m-0">{formatDate(selectedVM.createdAt)}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Updated</dt><dd className="m-0">{formatDate(selectedVM.updatedAt)}</dd>
                  </dl>
                </article>
              </section>

              {selectedVM.advancedOptions && (
                <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                  <h3 className="mb-[0.58rem] text-[1.05rem]">Advanced Options</h3>
                  <dl className="m-0 grid grid-cols-[150px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                    {selectedVM.advancedOptions.cpuMode && (
                      <><dt className="text-ink-soft text-[0.84rem]">CPU Mode</dt><dd className="m-0">{selectedVM.advancedOptions.cpuMode}</dd></>
                    )}
                    {selectedVM.advancedOptions.diskDriver && (
                      <><dt className="text-ink-soft text-[0.84rem]">Disk Driver</dt><dd className="m-0">{selectedVM.advancedOptions.diskDriver}</dd></>
                    )}
                    {selectedVM.advancedOptions.diskFormat && (
                      <><dt className="text-ink-soft text-[0.84rem]">Disk Format</dt><dd className="m-0">{selectedVM.advancedOptions.diskFormat}</dd></>
                    )}
                    {(selectedVM.advancedOptions.ioThreads ?? 0) > 0 && (
                      <><dt className="text-ink-soft text-[0.84rem]">IO Threads</dt><dd className="m-0">{selectedVM.advancedOptions.ioThreads}</dd></>
                    )}
                    {(selectedVM.advancedOptions.netMultiqueue ?? 0) > 0 && (
                      <><dt className="text-ink-soft text-[0.84rem]">Net Multiqueue</dt><dd className="m-0">{selectedVM.advancedOptions.netMultiqueue}</dd></>
                    )}
                    {selectedVM.advancedOptions.cpuPinning && Object.keys(selectedVM.advancedOptions.cpuPinning).length > 0 && (
                      <><dt className="text-ink-soft text-[0.84rem]">CPU Pinning</dt><dd className="m-0"><code className="text-[0.82rem]">{Object.entries(selectedVM.advancedOptions.cpuPinning).map(([v, c]) => `${v}:${c}`).join(', ')}</code></dd></>
                    )}
                  </dl>
                </section>
              )}

              {selectedVM.networkInterfaces && selectedVM.networkInterfaces.length > 0 && (
                <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                  <h3 className="mb-[0.58rem] text-[1.05rem]">Runtime Network Interfaces</h3>
                  <div className="overflow-auto">
                    <table>
                      <thead>
                        <tr>
                          <th>Name</th>
                          <th>MAC</th>
                          <th>IP Addresses</th>
                        </tr>
                      </thead>
                      <tbody>
                        {selectedVM.networkInterfaces.map((nic, i) => (
                          <tr key={`${nic.name || 'nic'}-${i}`}>
                            <td>{nic.name || '-'}</td>
                            <td><code className="text-[0.82rem]">{nic.mac || '-'}</code></td>
                            <td>{nic.ipAddresses && nic.ipAddresses.length > 0 ? nic.ipAddresses.join(', ') : '-'}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </section>
              )}
            </>
          )}
        </div>
      </section>
    </>
  )
}
