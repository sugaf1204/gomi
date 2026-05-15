import { useEffect, useRef, useState } from 'react'
import clsx from 'clsx'
import { api } from '../../api'
import type { GuardedAction, MachineTab } from '../../app-types'
import { phaseClass, powerStateClass, powerStateLabel } from '../../lib/formatters'
import type { AuditEvent, CloudInitTemplate, Hypervisor, Machine, PowerConfig, PowerType, SSHKey, Subnet } from '../../types'
import { ActivityTab } from './machine-tabs/ActivityTab'
import { ConfigurationTab } from './machine-tabs/ConfigurationTab'
import { ConsoleTab } from './machine-tabs/ConsoleTab'
import { DetailTab } from './machine-tabs/DetailTab'
import { InfoTab } from './machine-tabs/InfoTab'
import { MachineTabBar } from './machine-tabs/MachineTabBar'
import { NetworkTab } from './machine-tabs/NetworkTab'
import { SSHAccessFieldset } from './SSHAccessFieldset'
import { ModalOverlay } from '../ui/ModalOverlay'

type CloudInitInputMode = 'none' | 'existing' | 'create'

export type MachinesViewProps = {
  machineFilter: string
  onMachineFilterChange: (value: string) => void
  filteredMachines: Machine[]
  dataLoading: boolean
  selectedMachineName: string
  onSelectMachine: (name: string) => void
  onMachineUpsert: (machine: Machine) => void
  selectedMachine: Machine | null
  onOpenConfirm: (action: GuardedAction) => void
  onSaveMachineSettings: () => void | Promise<void>
  machineSettingsDirty: boolean
  machineSettingsSaving: boolean
  inlineEditField: 'power' | null
  onInlineEditFieldChange: (field: 'power' | null) => void
  machineSettingsPower: PowerConfig
  onMachineSettingsPowerChange: (value: PowerConfig) => void
  auditEvents: AuditEvent[]
  machineTab: MachineTab
  onMachineTabChange: (tab: MachineTab) => void
  subnets: Subnet[]
  onRefresh: () => void | Promise<void>
  hypervisors: Hypervisor[]
  osImages: import('../../types').OSImage[]
  cloudInits: CloudInitTemplate[]
  sshKeys: SSHKey[]
}

type MachinePrimaryAction = 'power-on' | 'power-off' | 'redeploy' | 'delete'

type BatchPowerAction = 'power-on' | 'power-off'

type BatchPowerConfirmState = {
  open: boolean
  action: BatchPowerAction
  targets: string[]
  running: boolean
}

const initialBatchPowerConfirmState: BatchPowerConfirmState = {
  open: false,
  action: 'power-on',
  targets: [],
  running: false
}

type BatchDeleteConfirmState = {
  open: boolean
  targets: string[]
  running: boolean
}

const initialBatchDeleteConfirmState: BatchDeleteConfirmState = {
  open: false,
  targets: [],
  running: false
}

type MachineFormState = {
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
}

type MachineDialogMode = 'create' | 'redeploy'

type MachineDialogState = {
  open: boolean
  mode: MachineDialogMode
  machineName: string
  running: boolean
}

function defaultSubnet(subnets: Subnet[]) {
  return subnets[0] ?? null
}

function createInitialMachineForm(subnets: Subnet[]): MachineFormState {
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
    loginUserPassword: ''
  }
}

function currentMachineCloudInitRef(machine: Machine | null) {
  if (!machine) return ''
  if (machine.lastDeployedCloudInitRef?.trim()) return machine.lastDeployedCloudInitRef.trim()
  if (machine.cloudInitRefs?.[0]?.trim()) return machine.cloudInitRefs[0].trim()
  return ''
}

function createMachineFormFromMachine(machine: Machine, subnets: Subnet[]): MachineFormState {
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
    loginUserPassword: ''
  }
}

function buildPowerConfig(form: MachineFormState): PowerConfig {
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

const initialMachineDialogState: MachineDialogState = {
  open: false,
  mode: 'create',
  machineName: '',
  running: false
}

export function MachinesView({
  machineFilter,
  onMachineFilterChange,
  filteredMachines,
  dataLoading,
  selectedMachineName,
  onSelectMachine,
  onMachineUpsert,
  selectedMachine,
  onOpenConfirm,
  onSaveMachineSettings,
  machineSettingsDirty,
  machineSettingsSaving,
  inlineEditField,
  onInlineEditFieldChange,
  machineSettingsPower,
  onMachineSettingsPowerChange,
  auditEvents,
  machineTab,
  onMachineTabChange,
  subnets,
  onRefresh,
  hypervisors,
  osImages,
  cloudInits,
  sshKeys
}: MachinesViewProps) {
  const [machineDialog, setMachineDialog] = useState<MachineDialogState>(initialMachineDialogState)
  const [selectedMachines, setSelectedMachines] = useState<Set<string>>(new Set())
  const multiSelectActive = selectedMachines.size > 0
  const [batchRunning, setBatchRunning] = useState(false)
  const [batchPowerConfirm, setBatchPowerConfirm] = useState<BatchPowerConfirmState>(initialBatchPowerConfirmState)
  const [batchDeleteConfirm, setBatchDeleteConfirm] = useState<BatchDeleteConfirmState>(initialBatchDeleteConfirmState)
  const [actionsMenuOpen, setActionsMenuOpen] = useState(false)
  const actionsMenuRef = useRef<HTMLDivElement | null>(null)
  const [form, setForm] = useState<MachineFormState>(() => createInitialMachineForm(subnets))

  function notifyError(message: string) {
    window.dispatchEvent(new CustomEvent('gomi:toast', { detail: { tone: 'error', message } }))
  }

  async function createInlineCloudInitTemplate(templateName: string, userData: string) {
    await api.createCloudInitTemplate({
      name: templateName,
      userData
    })
    return templateName
  }

  async function resolveCloudInitRefs(mode: CloudInitInputMode, existingRef: string, templateName: string, userData: string) {
    const trimmedExistingRef = existingRef.trim()
    const trimmedTemplateName = templateName.trim()
    const trimmedUserData = userData.trim()

    if (mode === 'none') return [] as string[]
    if (mode === 'existing') {
      return trimmedExistingRef ? [trimmedExistingRef] : []
    }
    if (!trimmedTemplateName || !trimmedUserData) {
      throw new Error('Cloud-Init inline creation requires both template name and user-data')
    }
    const createdName = await createInlineCloudInitTemplate(trimmedTemplateName, trimmedUserData)
    return [createdName]
  }

  async function buildMachineSpecPayload(formState: MachineFormState) {
    const cloudInitRefs = await resolveCloudInitRefs(
      formState.cloudInitMode,
      formState.cloudInitExistingRef,
      formState.cloudInitTemplateName,
      formState.cloudInitUserData
    )

    const machineNetwork = formState.domain
      ? { domain: formState.domain }
      : undefined

    return {
      hostname: formState.hostname,
      mac: formState.mac,
      arch: formState.arch,
      firmware: formState.firmware,
      power: buildPowerConfig(formState),
      osPreset: {
        family: formState.osFamily || 'linux',
        version: formState.osVersion || '',
        imageRef: formState.imageRef
      },
      targetDisk: formState.targetDisk.trim(),
      cloudInitRefs: cloudInitRefs.length > 0 ? cloudInitRefs : undefined,
      subnetRef: formState.subnetRef || undefined,
      ipAssignment: formState.subnetRef ? formState.ipAssignment : undefined,
      ip: formState.subnetRef && formState.ipAssignment === 'static' ? formState.staticIP : '',
      network: machineNetwork,
      role: formState.isHypervisor ? ('hypervisor' as const) : ('' as const),
      bridgeName: formState.isHypervisor ? (formState.bridgeName || 'br0') : '',
      sshKeyRefs: formState.sshKeyRefs,
      loginUser: formState.loginUserUsername.trim()
        ? {
            username: formState.loginUserUsername.trim(),
            password: formState.loginUserPassword.trim() || undefined
          }
        : undefined
    }
  }

  function closeMachineDialog() {
    setMachineDialog(initialMachineDialogState)
  }

  function openCreateDialog() {
    setForm(createInitialMachineForm(subnets))
    setMachineDialog({
      open: true,
      mode: 'create',
      machineName: '',
      running: false
    })
  }

  async function submitMachineDialog(e?: React.FormEvent) {
    e?.preventDefault()

    const currentDialog = machineDialog
    if (!form.imageRef) {
      notifyError('OS Image is required')
      return
    }

    setMachineDialog((current) => ({ ...current, running: true }))
    try {
      const payload = await buildMachineSpecPayload(form)
      if (currentDialog.mode === 'create') {
        const created = await api.createMachine({
          name: form.hostname,
          ...payload
        })
        onMachineUpsert(created)
        onSelectMachine(created.name)
      } else {
        const updated = await api.redeploy(currentDialog.machineName, {
          ...payload
        })
        onMachineUpsert(updated)
        onSelectMachine(updated.name)
      }
      setForm(createInitialMachineForm(subnets))
      closeMachineDialog()
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : `Failed to ${currentDialog.mode === 'create' ? 'create machine' : 'redeploy'}`)
      setMachineDialog((current) => ({ ...current, running: false }))
    }
  }

  useEffect(() => {
    if (!machineDialog.open && !batchPowerConfirm.open && !batchDeleteConfirm.open) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        if (!batchPowerConfirm.running) {
          setBatchPowerConfirm(initialBatchPowerConfirmState)
        }
        if (!batchDeleteConfirm.running) {
          setBatchDeleteConfirm(initialBatchDeleteConfirmState)
        }
        if (!machineDialog.running) {
          closeMachineDialog()
        }
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [machineDialog.open, machineDialog.running, batchPowerConfirm.open, batchPowerConfirm.running, batchDeleteConfirm.open, batchDeleteConfirm.running])

  useEffect(() => {
    if (!machineDialog.open || form.subnetRef || subnets.length === 0) return
    const subnet = defaultSubnet(subnets)
    if (!subnet) return
    setForm((current) => ({
      ...current,
      subnetRef: subnet.name,
      domain: current.domain || subnet.spec.domainName || ''
    }))
  }, [machineDialog.open, form.subnetRef, form.domain, subnets])

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

  function openRedeployDialog() {
    if (!selectedMachine) return
    setForm(createMachineFormFromMachine(selectedMachine, subnets))
    setMachineDialog({
      open: true,
      mode: 'redeploy',
      machineName: selectedMachine.name,
      running: false
    })
  }

  function runPrimaryAction(action: MachinePrimaryAction) {
    const targets = selectedMachines.size > 0
      ? Array.from(selectedMachines)
      : (selectedMachine ? [selectedMachine.name] : [])
    if (targets.length === 0) return

    switch (action) {
      case 'power-on':
        openPowerConfirm('power-on', targets)
        break
      case 'power-off':
        openPowerConfirm('power-off', targets)
        break
      case 'redeploy':
        openRedeployDialog()
        break
      case 'delete':
        if (selectedMachines.size > 0) {
          openBatchDeleteConfirm()
        } else {
          onOpenConfirm('delete')
        }
        break
    }
  }

  const hypervisorByHost = new Map(hypervisors.map((hv) => [hv.connection.host, hv]))
  function findHypervisorForMachine(machine: Machine): Hypervisor | undefined {
    return hypervisorByHost.get(machine.hostname) ?? hypervisorByHost.get(machine.ip ?? '') ?? hypervisors.find((hv) => hv.name === machine.name)
  }

  function openPowerConfirm(action: BatchPowerAction, explicitTargets?: string[]) {
    const targets = explicitTargets && explicitTargets.length > 0
      ? explicitTargets
      : Array.from(selectedMachines)
    if (targets.length === 0) return
    setBatchPowerConfirm({
      open: true,
      action,
      targets: [...targets].sort(),
      running: false
    })
  }

  async function submitBatchPowerConfirm() {
    if (batchPowerConfirm.targets.length === 0) return

    const action = batchPowerConfirm.action
    const targets = [...batchPowerConfirm.targets]

    setBatchRunning(true)
    setBatchPowerConfirm((current) => ({ ...current, running: true }))

    try {
      const failures: string[] = []
      for (const name of targets) {
        try {
          if (action === 'power-on') {
            await api.powerOn(name)
          } else {
            await api.powerOff(name)
          }
        } catch {
          failures.push(name)
        }
      }

      if (failures.length > 0) {
        notifyError(`Failed to ${action === 'power-on' ? 'power on' : 'power off'}: ${failures.join(', ')}`)
        setSelectedMachines(new Set(failures))
      } else {
        setSelectedMachines(new Set())
      }

      setBatchPowerConfirm(initialBatchPowerConfirmState)
      void onRefresh()
    } finally {
      setBatchRunning(false)
    }
  }

  function openBatchDeleteConfirm() {
    if (selectedMachines.size === 0) return
    setBatchDeleteConfirm({
      open: true,
      targets: [...selectedMachines].sort(),
      running: false
    })
  }

  async function submitBatchDeleteConfirm() {
    if (batchDeleteConfirm.targets.length === 0) return

    const targets = [...batchDeleteConfirm.targets]
    setBatchRunning(true)
    setBatchDeleteConfirm((current) => ({ ...current, running: true }))

    try {
      const failures: string[] = []
      for (const name of targets) {
        try {
          await api.deleteMachine(name)
        } catch {
          failures.push(name)
        }
      }

      if (failures.length > 0) {
        notifyError(`Failed to delete: ${failures.join(', ')}`)
        setSelectedMachines(new Set(failures))
      } else {
        setSelectedMachines(new Set())
      }

      setBatchDeleteConfirm(initialBatchDeleteConfirmState)
      void onRefresh()
    } finally {
      setBatchRunning(false)
    }
  }

  function toggleMachineSelect(name: string) {
    setSelectedMachines((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  function toggleSelectAll() {
    if (selectedMachines.size === filteredMachines.length) {
      setSelectedMachines(new Set())
    } else {
      setSelectedMachines(new Set(filteredMachines.map((m) => m.name)))
    }
  }

  function renderMachineSpecFields(radioName: string) {
    return (
      <>
        <label className="text-[0.84rem]">
          Hostname
          <input required value={form.hostname} onChange={(e) => setForm((current) => ({ ...current, hostname: e.target.value }))} placeholder="e.g. node1" />
        </label>
        <label className="text-[0.84rem]">
          MAC Address
          <input
            required
            value={form.mac}
            onChange={(e) => setForm((current) => ({ ...current, mac: e.target.value }))}
            placeholder="aa:bb:cc:dd:ee:ff"
          />
        </label>
        <label className="text-[0.84rem]">
          Power Control Type
          <select required value={form.powerType} onChange={(e) => setForm((current) => ({ ...current, powerType: e.target.value as PowerType }))}>
            <option value="manual">Manual</option>
            <option value="ipmi">IPMI</option>
            <option value="webhook">Webhook</option>
            <option value="wol">Wake-on-LAN</option>
          </select>
        </label>
        {form.powerType === 'ipmi' && (
          <div className="grid gap-[0.45rem] pl-[0.4rem] border-l-2 border-brand/30">
            <label className="text-[0.84rem]">
              BMC Host
              <input required value={form.ipmiHost} onChange={(e) => setForm((current) => ({ ...current, ipmiHost: e.target.value }))} placeholder="10.0.0.1" />
            </label>
            <div className="grid grid-cols-2 gap-[0.55rem]">
              <label className="text-[0.84rem]">
                Username
                <input required value={form.ipmiUsername} onChange={(e) => setForm((current) => ({ ...current, ipmiUsername: e.target.value }))} placeholder="admin" />
              </label>
              <label className="text-[0.84rem]">
                Password
                <input type="password" required value={form.ipmiPassword} onChange={(e) => setForm((current) => ({ ...current, ipmiPassword: e.target.value }))} />
              </label>
            </div>
          </div>
        )}
        {form.powerType === 'webhook' && (
          <div className="grid gap-[0.45rem] pl-[0.4rem] border-l-2 border-brand/30">
            <label className="text-[0.84rem]">
              Power On URL
              <input required value={form.webhookOnURL} onChange={(e) => setForm((current) => ({ ...current, webhookOnURL: e.target.value }))} placeholder="https://..." />
            </label>
            <label className="text-[0.84rem]">
              Power Off URL
              <input required value={form.webhookOffURL} onChange={(e) => setForm((current) => ({ ...current, webhookOffURL: e.target.value }))} placeholder="https://..." />
            </label>
            <label className="text-[0.84rem]">
              Status URL <span className="text-ink-soft">(optional)</span>
              <input value={form.webhookStatusURL} onChange={(e) => setForm((current) => ({ ...current, webhookStatusURL: e.target.value }))} placeholder="https://..." />
            </label>
            <label className="text-[0.84rem]">
              Boot Order URL <span className="text-ink-soft">(optional, BIOS deploy)</span>
              <input value={form.webhookBootOrderURL} onChange={(e) => setForm((current) => ({ ...current, webhookBootOrderURL: e.target.value }))} placeholder="https://..." />
            </label>
          </div>
        )}
        {form.powerType === 'wol' && (
          <div className="grid gap-[0.45rem] pl-[0.4rem] border-l-2 border-brand/30">
            <label className="text-[0.84rem]">
              Wake MAC <span className="text-ink-soft">(defaults to machine MAC)</span>
              <input value={form.wolMAC} onChange={(e) => setForm((current) => ({ ...current, wolMAC: e.target.value }))} placeholder={form.mac || 'aa:bb:cc:dd:ee:ff'} />
            </label>
          </div>
        )}
        <div className="grid grid-cols-2 gap-[0.55rem]">
          <label className="text-[0.84rem]">
            Arch
            <select value={form.arch} onChange={(e) => setForm((current) => ({ ...current, arch: e.target.value }))}>
              <option value="amd64">amd64</option>
              <option value="arm64">arm64</option>
            </select>
          </label>
          <label className="text-[0.84rem]">
            Firmware
            <select value={form.firmware} onChange={(e) => setForm((current) => ({ ...current, firmware: e.target.value as 'uefi' | 'bios' }))}>
              <option value="uefi">UEFI</option>
              <option value="bios">BIOS</option>
            </select>
          </label>
        </div>
        <label className="text-[0.84rem] flex items-center gap-2 cursor-pointer select-none">
          <input type="checkbox" checked={form.isHypervisor} onChange={(e) => setForm((current) => ({ ...current, isHypervisor: e.target.checked }))} />
          Register as Hypervisor
        </label>
        {form.isHypervisor && (
          <label className="text-[0.84rem]">
            Bridge Name
            <input type="text" value={form.bridgeName} onChange={(e) => setForm((current) => ({ ...current, bridgeName: e.target.value }))} placeholder="br0" />
          </label>
        )}
        <label className="text-[0.84rem]">
          OS Image
          <select value={form.imageRef} onChange={(e) => {
            const selected = osImages.find((img) => img.name === e.target.value)
            setForm((current) => ({ ...current, imageRef: e.target.value, osFamily: selected?.osFamily || '', osVersion: selected?.osVersion || '' }))
          }}>
            <option value="">None</option>
            {osImages.map((img) => (
              <option key={img.name} value={img.name}>{img.name} ({img.osFamily} {img.osVersion})</option>
            ))}
          </select>
        </label>
        <label className="text-[0.84rem]">
          Target Disk <span className="text-ink-soft">(optional)</span>
          <input value={form.targetDisk} onChange={(e) => setForm((current) => ({ ...current, targetDisk: e.target.value }))} placeholder="/dev/disk/by-id/..." />
        </label>
        <label className="text-[0.84rem]">
          Cloud-Init <span className="text-ink-soft">(optional)</span>
          <select
            value={form.cloudInitMode}
            onChange={(e) => setForm((current) => ({ ...current, cloudInitMode: e.target.value as CloudInitInputMode }))}
          >
            <option value="none">None</option>
            <option value="existing">Use existing template</option>
            <option value="create">Create template inline</option>
          </select>
        </label>
        {form.cloudInitMode === 'existing' && (
          <label className="text-[0.84rem]">
            Existing Cloud-Init Template
            <select
              value={form.cloudInitExistingRef}
              onChange={(e) => setForm((current) => ({ ...current, cloudInitExistingRef: e.target.value }))}
            >
              <option value="">None</option>
              {cloudInits.map((ci) => (
                <option key={ci.name} value={ci.name}>{ci.name}</option>
              ))}
            </select>
          </label>
        )}
        {form.cloudInitMode === 'existing' && cloudInits.length === 0 && (
          <p className="m-0 text-[0.78rem] text-ink-soft">No Cloud-Init templates available. Switch mode to create one inline.</p>
        )}
        {form.cloudInitMode === 'create' && (
          <>
            <label className="text-[0.84rem]">
              Cloud-Init Template Name
              <input
                required
                value={form.cloudInitTemplateName}
                onChange={(e) => setForm((current) => ({ ...current, cloudInitTemplateName: e.target.value }))}
                placeholder="e.g. ci-node1"
              />
            </label>
            <label className="text-[0.84rem]">
              Cloud-Init User Data
              <textarea
                required
                className="min-h-[140px] font-mono text-[0.78rem]"
                value={form.cloudInitUserData}
                onChange={(e) => setForm((current) => ({ ...current, cloudInitUserData: e.target.value }))}
                placeholder="#cloud-config\nusers:\n  - default"
              />
            </label>
          </>
        )}
        <SSHAccessFieldset
          sshKeys={sshKeys}
          value={form}
          onChange={(updater) => setForm((current) => ({ ...current, ...updater(current) }))}
          onRefresh={onRefresh}
        />
        <label className="text-[0.84rem]">
          Subnet {subnets.length === 0 && <span className="text-ink-soft">(optional)</span>}
          <select value={form.subnetRef} onChange={(e) => {
            const subnetName = e.target.value
            const subnet = subnets.find((item) => item.name === subnetName)
            setForm((current) => ({
              ...current,
              subnetRef: subnetName,
              ipAssignment: subnetName ? current.ipAssignment : 'dhcp',
              staticIP: subnetName ? current.staticIP : '',
              domain: subnet?.spec.domainName || ''
            }))
          }}>
            {subnets.length === 0 && <option value="">None</option>}
            {subnets.map((subnet) => (
              <option key={subnet.name} value={subnet.name}>{subnet.name} ({subnet.spec.cidr})</option>
            ))}
          </select>
        </label>
        {form.subnetRef && (
          <div className="grid gap-[0.45rem] pl-[0.4rem] border-l-2 border-brand/30">
            <div className="flex items-center gap-[0.8rem] text-[0.84rem]">
              <span>IP Assignment:</span>
              <label className="flex items-center gap-1 cursor-pointer">
                <input type="radio" name={`${radioName}-ip-assignment`} checked={form.ipAssignment === 'dhcp'} onChange={() => setForm((current) => ({ ...current, ipAssignment: 'dhcp' }))} />
                DHCP
              </label>
              <label className="flex items-center gap-1 cursor-pointer">
                <input type="radio" name={`${radioName}-ip-assignment`} checked={form.ipAssignment === 'static'} onChange={() => setForm((current) => ({ ...current, ipAssignment: 'static' }))} />
                Static
              </label>
            </div>
            {form.ipAssignment === 'static' && (
              <label className="text-[0.84rem]">
                Static IP
                <input value={form.staticIP} onChange={(e) => setForm((current) => ({ ...current, staticIP: e.target.value }))} placeholder="e.g. 10.0.0.50" />
              </label>
            )}
            <label className="text-[0.84rem]">
              Domain
              <input value={form.domain} onChange={(e) => setForm((current) => ({ ...current, domain: e.target.value }))} placeholder="e.g. example.local" />
            </label>
          </div>
        )}
      </>
    )
  }

  return (
    <>
      {machineDialog.open && (
        <ModalOverlay onBackdropClick={() => {
          if (!machineDialog.running) {
            closeMachineDialog()
          }
        }}>
          <div className="w-[min(620px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">{machineDialog.mode === 'create' ? 'Add Machine' : `Redeploy: ${machineDialog.machineName}`}</h3>
              <button
                aria-label="Close"
                className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                disabled={machineDialog.running}
                onClick={() => { closeMachineDialog() }}
              >×</button>
            </div>
            {machineDialog.mode === 'redeploy' && selectedMachine && (
              <div className="grid gap-[0.3rem] bg-[#f9f7f4] border border-line p-[0.6rem] text-[0.84rem]">
                <p className="m-0 text-ink-soft">OS Preset: <strong className="text-ink">{selectedMachine.osPreset.family} {selectedMachine.osPreset.version}</strong></p>
                <p className="m-0 text-ink-soft">Current Phase: <strong className="text-ink">{selectedMachine.phase}</strong></p>
                {(selectedMachine.phase.toLowerCase() === 'running' || selectedMachine.phase.toLowerCase() === 'ready') && (
                  <p className="m-0 text-[#8a4f23] font-medium mt-[0.15rem]">This machine is currently active. Redeploying will disrupt services.</p>
                )}
              </div>
            )}
            <form className="grid gap-[0.55rem]" onSubmit={(e) => void submitMachineDialog(e)}>
              {renderMachineSpecFields(`machine-${machineDialog.mode}`)}
              <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
                <button type="button" onClick={() => { closeMachineDialog() }} disabled={machineDialog.running}>Cancel</button>
                <button
                  type="submit"
                  disabled={
                    machineDialog.running
                    || !form.hostname.trim()
                    || !form.mac.trim()
                    || !form.imageRef
                    || (form.subnetRef && form.ipAssignment === 'static' && !form.staticIP.trim())
                    || (form.cloudInitMode === 'create' && (!form.cloudInitTemplateName.trim() || !form.cloudInitUserData.trim()))
                  }
                  className={clsx(
                    'text-white',
                    machineDialog.mode === 'create' ? 'bg-brand border-brand-strong' : 'bg-[#d86b6b] border-[#be5252]'
                  )}
                >
                  {machineDialog.running
                    ? (machineDialog.mode === 'create' ? 'Creating...' : 'Redeploying...')
                    : (machineDialog.mode === 'create' ? 'Create Machine' : 'Redeploy')}
                </button>
              </div>
            </form>
          </div>
        </ModalOverlay>
      )}

      {batchPowerConfirm.open && (
        <ModalOverlay onBackdropClick={() => {
          if (!batchPowerConfirm.running) {
            setBatchPowerConfirm(initialBatchPowerConfirmState)
          }
        }}>
          <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
            <h3 className="text-[1.2rem]">{batchPowerConfirm.action === 'power-on' ? 'Confirm Power On' : 'Confirm Power Off'}</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">Target machines ({batchPowerConfirm.targets.length}):</p>
            <div className="max-h-[180px] overflow-auto border border-line p-[0.55rem] bg-[#f9f7f4]">
              <ul className="m-0 pl-[1.1rem]">
                {batchPowerConfirm.targets.map((target) => (
                  <li key={target}><code>{target}</code></li>
                ))}
              </ul>
            </div>
            <div className="flex justify-end gap-[0.45rem]">
              <button type="button" onClick={() => setBatchPowerConfirm(initialBatchPowerConfirmState)} disabled={batchPowerConfirm.running}>Cancel</button>
              <button
                type="button"
                onClick={() => void submitBatchPowerConfirm()}
                disabled={batchPowerConfirm.running}
                className={clsx(
                  'text-white',
                  batchPowerConfirm.action === 'power-on' ? 'bg-brand border-brand-strong' : 'bg-[#d86b6b] border-[#be5252]'
                )}
              >
                {batchPowerConfirm.running
                  ? (batchPowerConfirm.action === 'power-on' ? 'Powering On...' : 'Powering Off...')
                  : (batchPowerConfirm.action === 'power-on' ? 'Power On' : 'Power Off')}
              </button>
            </div>
          </div>
        </ModalOverlay>
      )}

      {batchDeleteConfirm.open && (
        <ModalOverlay onBackdropClick={() => {
          if (!batchDeleteConfirm.running) {
            setBatchDeleteConfirm(initialBatchDeleteConfirmState)
          }
        }}>
          <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
            <h3 className="text-[1.2rem] text-[#9b2d2d]">Confirm Batch Delete</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">Target machines ({batchDeleteConfirm.targets.length}):</p>
            <div className="max-h-[180px] overflow-auto border border-line p-[0.55rem] bg-[#f9f7f4]">
              <ul className="m-0 pl-[1.1rem]">
                {batchDeleteConfirm.targets.map((target) => (
                  <li key={target}><code>{target}</code></li>
                ))}
              </ul>
            </div>
            <div className="flex justify-end gap-[0.45rem]">
              <button type="button" onClick={() => setBatchDeleteConfirm(initialBatchDeleteConfirmState)} disabled={batchDeleteConfirm.running}>Cancel</button>
              <button
                type="button"
                onClick={() => void submitBatchDeleteConfirm()}
                disabled={batchDeleteConfirm.running}
                className="bg-[#d86b6b] border-[#be5252] text-white"
              >
                {batchDeleteConfirm.running ? 'Deleting...' : 'Delete'}
              </button>
            </div>
          </div>
        </ModalOverlay>
      )}

      <section className="min-h-0 grid grid-cols-1 md:grid-cols-[310px_minmax(0,1fr)] gap-[0.9rem]">
        <div className="min-h-0 grid grid-rows-[auto_minmax(0,1fr)] gap-[0.6rem]">
          <div className="grid gap-[0.45rem]">
            <div className="flex justify-between items-center gap-2">
              <h2 className="text-[1.4rem]">Machine List</h2>
              <button
                className="bg-brand border-brand-strong text-white py-[0.35rem] px-[0.55rem] text-[0.82rem]"
                onClick={openCreateDialog}
              >
                Add
              </button>
            </div>
            <input
              aria-label="Machine filter"
              placeholder="Search name, host, MAC"
              value={machineFilter}
              onChange={(e) => onMachineFilterChange(e.target.value)}
            />
          </div>

          <div className="overflow-auto border-t border-line pr-[0.1rem]">
            {dataLoading && filteredMachines.length === 0 && (
              <div className="grid place-items-center py-[2.4rem] text-center text-ink-soft gap-[0.55rem]">
                <div className="loading-spinner" aria-hidden="true" />
                <p className="m-0 text-[0.84rem]">Loading machines...</p>
              </div>
            )}
            {filteredMachines.length > 0 && (
              <div className="flex items-center gap-[0.45rem] py-[0.35rem] pl-[0.55rem] border-b border-line bg-[#f9f7f4]">
                <input
                  type="checkbox"
                  className="w-[0.95rem] h-[0.95rem] m-0 shrink-0 accent-[#2b7a78] cursor-pointer"
                  checked={selectedMachines.size === filteredMachines.length && filteredMachines.length > 0}
                  onChange={toggleSelectAll}
                />
                <span className="text-[0.78rem] text-ink-soft">Select All</span>
              </div>
            )}
            {filteredMachines.map((machine) => (
              <div
                key={machine.name}
                className={clsx(
                  'flex items-start border-0 border-b border-line border-l-[3px] border-l-transparent',
                  selectedMachineName === machine.name && '!border-l-brand bg-[rgba(43,122,120,0.06)]'
                )}
              >
                <div className="flex items-center pl-[0.55rem] pt-[0.72rem] shrink-0">
                  <input
                    type="checkbox"
                    className="w-[0.95rem] h-[0.95rem] m-0 accent-[#2b7a78] cursor-pointer"
                    checked={selectedMachines.has(machine.name)}
                    onChange={() => toggleMachineSelect(machine.name)}
                  />
                </div>
                <button
                  className="text-left flex-1 min-w-0 border-0 bg-transparent flex justify-between items-start gap-[0.75rem] py-[0.62rem] pl-[0.45rem] pr-[0.1rem] shadow-none hover:transform-none!"
                  onClick={() => onSelectMachine(machine.name)}
                >
                  <div className="min-w-0">
                    <p className="m-0 font-ui font-medium tracking-normal truncate">{machine.name}</p>
                    <p className="m-0 text-ink-soft text-[0.82rem]">{machine.osPreset.family} {machine.osPreset.version} - {machine.firmware.toUpperCase()}</p>
                  </div>
                  {machine.role === 'hypervisor' && <span className="text-[0.7rem] px-1.5 py-0.5 rounded bg-purple-100 text-purple-700 font-medium shrink-0">HV</span>}
                  <span className={clsx(phaseClass(machine.phase), 'shrink-0')}>{machine.phase}</span>
                </button>
              </div>
            ))}
            {!dataLoading && filteredMachines.length === 0 && <p className="m-0 text-ink-soft">No machines found</p>}
          </div>
        </div>

        <div className="min-h-0 grid content-start gap-[0.85rem]">
          {!selectedMachine && !multiSelectActive && (
            <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.45rem]">
              <h2>Select a machine</h2>
              <p>Choose a machine from the index to view actions, details, and recent operations.</p>
            </section>
          )}

          {(selectedMachine || multiSelectActive) && (
            <>
              <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid grid-cols-[minmax(0,1fr)_auto] items-start gap-[0.85rem] max-sm:grid-cols-1">
                <div className="min-w-0">
                  <p className="m-0 font-ui font-medium text-[0.72rem] uppercase tracking-[0.08em] text-ink-soft">Bare Metal Machine</p>
                  {multiSelectActive ? (
                    <h2 className="mt-[0.22rem] text-[1.8rem] break-anywhere">{selectedMachines.size} selected</h2>
                  ) : selectedMachine && (
                    <>
                      <h2 className="mt-[0.22rem] text-[1.8rem] break-anywhere">{selectedMachine.name}</h2>
                      <p className="m-0 text-ink-soft break-anywhere">{selectedMachine.hostname} - {selectedMachine.arch} - {selectedMachine.firmware.toUpperCase()}</p>
                      <p className="m-0 mt-[0.15rem] text-[0.84rem] flex items-center gap-[0.4rem]">
                        <span className="text-ink-soft">IP:</span>
                        <span>{selectedMachine.ip || '-'}</span>
                        <span className={clsx(
                          'text-[0.68rem] font-medium px-[0.35rem] py-[0.05rem] rounded-sm border',
                          selectedMachine.ipAssignment === 'static'
                            ? 'bg-[#e8f4f3] text-[#1a6360] border-[#b8dbd9]'
                            : 'bg-[#f3f0ea] text-ink-soft border-[#e0dbd2]'
                        )}>
                          {selectedMachine.ipAssignment === 'static' ? 'Static' : 'DHCP'}
                        </span>
                      </p>
                      {selectedMachine.powerState && (
                        <p className="m-0 mt-[0.25rem]">
                          <span className={powerStateClass(selectedMachine.powerState)}>{powerStateLabel(selectedMachine.powerState)}</span>
                        </p>
                      )}
                      {(() => {
                        const hv = findHypervisorForMachine(selectedMachine)
                        if (!hv) return null
                        return (
                          <p className="m-0 mt-[0.25rem] text-[0.82rem]">
                            <span className="inline-flex items-center font-ui rounded-full text-[0.68rem] font-semibold px-2 py-0.5 bg-[#e8e0f6] text-[#5b3d8f]">Hypervisor</span>
                            <span className="text-ink-soft ml-[0.4rem]">{hv.vmCount} VM{hv.vmCount !== 1 ? 's' : ''} hosted</span>
                          </p>
                        )
                      })()}
                    </>
                  )}
                </div>
                <div className="flex min-w-0 flex-col items-end gap-[0.55rem] max-sm:items-start">
                  {!multiSelectActive && selectedMachine && (
                    <span className={phaseClass(selectedMachine.phase)}>{selectedMachine.phase}</span>
                  )}
                  <div className="flex justify-end items-center flex-wrap gap-[0.35rem]" ref={actionsMenuRef}>
                    <div className="relative">
                      <button
                        className="py-[0.45rem] px-[0.72rem]"
                        disabled={selectedMachines.size === 0 && !selectedMachine}
                        onClick={() => setActionsMenuOpen((current) => !current)}
                      >
                        Actions
                      </button>
                      {actionsMenuOpen && (
                        <div className="absolute right-0 mt-1 min-w-[180px] bg-white border border-line shadow-[0_10px_24px_rgba(52,43,34,0.16)] z-10">
                          {([
                            { value: 'power-on', label: 'Power On' },
                            { value: 'power-off', label: 'Power Off' },
                            { value: 'redeploy', label: 'Redeploy' },
                            { value: 'delete', label: 'Delete' }
                          ] as Array<{ value: MachinePrimaryAction, label: string }>).map((item) => (
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

              {selectedMachine && (
                <>
                  <MachineTabBar activeTab={machineTab} onTabChange={onMachineTabChange} />

                  {machineTab === 'info' && <InfoTab machine={selectedMachine} />}
                  {machineTab === 'detail' && <DetailTab machine={selectedMachine} />}
                  {machineTab === 'network' && <NetworkTab machine={selectedMachine} subnets={subnets} onRefresh={onRefresh} />}
                  {machineTab === 'console' && <ConsoleTab machine={selectedMachine} />}
                  {machineTab === 'activity' && <ActivityTab auditEvents={auditEvents} />}
                  {machineTab === 'configuration' && (
                    <ConfigurationTab
                      machine={selectedMachine}
                      onSaveMachineSettings={onSaveMachineSettings}
                      machineSettingsDirty={machineSettingsDirty}
                      machineSettingsSaving={machineSettingsSaving}
                      inlineEditField={inlineEditField}
                      onInlineEditFieldChange={onInlineEditFieldChange}
                      machineSettingsPower={machineSettingsPower}
                      onMachineSettingsPowerChange={onMachineSettingsPowerChange}
                    />
                  )}
                </>
              )}
            </>
          )}
        </div>
      </section>
    </>
  )
}
