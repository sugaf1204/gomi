import type { Dispatch, FormEvent, SetStateAction } from 'react'
import { api } from '../../../api'
import type { Hypervisor, Machine, OSImage, Subnet } from '../../../types'
import {
  buildMachineLoginUserPayload,
  buildPowerConfig,
  createInitialMachineForm,
  createMachineFormFromMachine,
  currentMachineCloudInitRefs,
  initialBatchDeleteConfirmState,
  initialBatchPowerConfirmState,
  initialBatchRedeployConfirmState,
  initialMachineDialogState,
  mergeSelectedCloudInitRef
} from './machineFormState'
import type {
  BatchDeleteConfirmState,
  BatchPowerAction,
  BatchPowerConfirmState,
  BatchRedeployConfirmState,
  CloudInitInputMode,
  MachineDialogState,
  MachineFormState,
  MachinePrimaryAction,
  MachineQuickDeployPreset
} from './machineFormState'

type MachineOperationsArgs = {
  machineDialog: MachineDialogState
  setMachineDialog: Dispatch<SetStateAction<MachineDialogState>>
  form: MachineFormState
  setForm: Dispatch<SetStateAction<MachineFormState>>
  quickDeployPreset: MachineQuickDeployPreset
  setQuickDeployPreset: Dispatch<SetStateAction<MachineQuickDeployPreset>>
  setQuickDeploySettingsOpen: Dispatch<SetStateAction<boolean>>
  setQuickDeploying: Dispatch<SetStateAction<boolean>>
  selectedMachines: Set<string>
  setSelectedMachines: Dispatch<SetStateAction<Set<string>>>
  batchPowerConfirm: BatchPowerConfirmState
  setBatchPowerConfirm: Dispatch<SetStateAction<BatchPowerConfirmState>>
  batchDeleteConfirm: BatchDeleteConfirmState
  setBatchDeleteConfirm: Dispatch<SetStateAction<BatchDeleteConfirmState>>
  batchRedeployConfirm: BatchRedeployConfirmState
  setBatchRedeployConfirm: Dispatch<SetStateAction<BatchRedeployConfirmState>>
  setBatchRunning: Dispatch<SetStateAction<boolean>>
  selectedMachine: Machine | null
  machines: Machine[]
  filteredMachines: Machine[]
  osImages: OSImage[]
  subnets: Subnet[]
  hypervisors: Hypervisor[]
  onMachineUpsert: (machine: Machine) => void
  onSelectMachine: (name: string) => void
  onOpenSingleDeleteConfirm: () => void
  onRefresh: () => void | Promise<void>
}

export function useMachineOperations(args: MachineOperationsArgs) {
  const notifyError = (message: string) => {
    window.dispatchEvent(new CustomEvent('gomi:toast', { detail: { tone: 'error', message } }))
  }

  const quickDeployMachineName = (preset: MachineQuickDeployPreset) => `${preset.name.trim()}-${Math.max(1, Number(preset.count) || 1)}`

  const quickDeployMachineReady = (preset: MachineQuickDeployPreset) => {
    if (!preset.name.trim()) return false
    if ((Number(preset.count) || 0) < 1) return false
    if (!preset.mac.trim()) return false
    if (!preset.imageRef.trim()) return false
    if (!args.osImages.some((img) => img.name === preset.imageRef)) return false
    if (preset.subnetRef && preset.ipAssignment === 'static' && !preset.staticIP.trim()) return false
    if (preset.powerType === 'ipmi' && (!preset.ipmiHost.trim() || !preset.ipmiUsername.trim() || !preset.ipmiPassword.trim())) return false
    if (preset.powerType === 'webhook' && (!preset.webhookOnURL.trim() || !preset.webhookOffURL.trim())) return false
    return true
  }

  function quickDeployMachineFormState(preset: MachineQuickDeployPreset): MachineFormState {
    return {
      ...preset,
      hostname: quickDeployMachineName(preset),
      cloudInitMode: preset.cloudInitExistingRef ? 'existing' : 'none',
      cloudInitTemplateName: '',
      cloudInitUserData: '',
      loginUserPasswordTouched: false
    }
  }

  async function resolveCloudInitRefs(mode: CloudInitInputMode, existingRef: string, templateName: string, userData: string, currentRefs: string[] = []) {
    const trimmedExistingRef = existingRef.trim()
    const trimmedTemplateName = templateName.trim()
    const trimmedUserData = userData.trim()
    if (mode === 'none') return [] as string[]
    if (mode === 'existing') return mergeSelectedCloudInitRef(trimmedExistingRef, currentRefs)
    if (!trimmedTemplateName || !trimmedUserData) throw new Error('Cloud-Init inline creation requires both template name and user-data')
    await api.createCloudInitTemplate({ name: trimmedTemplateName, userData: trimmedUserData })
    return mergeSelectedCloudInitRef(trimmedTemplateName, currentRefs)
  }

  async function buildMachineSpecPayload(formState: MachineFormState, currentMachine?: Machine) {
    const cloudInitRefs = await resolveCloudInitRefs(
      formState.cloudInitMode,
      formState.cloudInitExistingRef,
      formState.cloudInitTemplateName,
      formState.cloudInitUserData,
      currentMachineCloudInitRefs(currentMachine)
    )
    return {
      hostname: formState.hostname,
      mac: formState.mac,
      arch: formState.arch,
      firmware: formState.firmware,
      power: buildPowerConfig(formState),
      osPreset: { family: formState.osFamily || 'linux', version: formState.osVersion || '', imageRef: formState.imageRef },
      targetDisk: formState.targetDisk.trim(),
      cloudInitRefs: cloudInitRefs.length > 0 ? cloudInitRefs : undefined,
      subnetRef: formState.subnetRef || undefined,
      ipAssignment: formState.subnetRef ? formState.ipAssignment : undefined,
      ip: formState.subnetRef && formState.ipAssignment === 'static' ? formState.staticIP : '',
      network: formState.domain ? { domain: formState.domain } : undefined,
      role: formState.isHypervisor ? ('hypervisor' as const) : ('' as const),
      bridgeName: formState.isHypervisor ? (formState.bridgeName || 'br0') : '',
      sshKeyRefs: formState.sshKeyRefs,
      loginUser: buildMachineLoginUserPayload(formState, currentMachine?.loginUser)
    }
  }

  const closeMachineDialog = () => args.setMachineDialog(initialMachineDialogState)

  function openCreateDialog() {
    args.setForm(createInitialMachineForm(args.subnets))
    args.setMachineDialog({ open: true, mode: 'create', machineName: '', running: false })
  }

  async function submitMachineDialog(e?: FormEvent) {
    e?.preventDefault()
    const currentDialog = args.machineDialog
    if (!args.form.imageRef) return notifyError('OS Image is required')
    args.setMachineDialog((current) => ({ ...current, running: true }))
    try {
      if (currentDialog.mode === 'create') {
        const created = await api.createMachine({ name: args.form.hostname, ...(await buildMachineSpecPayload(args.form)) })
        args.onMachineUpsert(created)
        args.onSelectMachine(created.name)
      } else {
        const updated = await api.redeploy(currentDialog.machineName, await buildMachineSpecPayload(args.form, args.selectedMachine ?? undefined))
        args.onMachineUpsert(updated)
        args.onSelectMachine(updated.name)
      }
      args.setForm(createInitialMachineForm(args.subnets))
      closeMachineDialog()
      args.onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : `Failed to ${currentDialog.mode === 'create' ? 'create machine' : 'redeploy'}`)
      args.setMachineDialog((current) => ({ ...current, running: false }))
    }
  }

  async function handleQuickDeploy() {
    const preset = args.quickDeployPreset
    if (!quickDeployMachineReady(preset)) {
      args.setQuickDeploySettingsOpen(true)
      return
    }
    const machineName = quickDeployMachineName(preset)
    args.setQuickDeploying(true)
    try {
      const created = await api.createMachine({ name: machineName, ...(await buildMachineSpecPayload(quickDeployMachineFormState(preset))) })
      args.onMachineUpsert(created)
      args.onSelectMachine(created.name)
      args.setQuickDeployPreset((current) => ({ ...current, count: String(Math.max(1, Number(current.count) || 1) + 1) }))
      args.onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to quick deploy machine')
    } finally {
      args.setQuickDeploying(false)
    }
  }

  function openRedeployDialog() {
    if (!args.selectedMachine) return
    args.setForm(createMachineFormFromMachine(args.selectedMachine, args.subnets))
    args.setMachineDialog({ open: true, mode: 'redeploy', machineName: args.selectedMachine.name, running: false })
  }

  function openBatchRedeployDialog(targets: string[]) {
    const sortedTargets = [...targets].sort()
    const forms: Record<string, MachineFormState> = {}
    const status: BatchRedeployConfirmState['status'] = {}
    for (const target of sortedTargets) {
      const machine = args.machines.find((item) => item.name === target)
      if (!machine) continue
      forms[target] = createMachineFormFromMachine(machine, args.subnets)
      status[target] = { state: 'pending' }
    }
    const availableTargets = sortedTargets.filter((target) => forms[target])
    if (availableTargets.length === 0) return
    args.setBatchRedeployConfirm({ open: true, targets: availableTargets, activeTarget: availableTargets[0], forms, status, running: false })
  }

  function runPrimaryAction(action: MachinePrimaryAction) {
    const targets = args.selectedMachines.size > 0 ? Array.from(args.selectedMachines) : (args.selectedMachine ? [args.selectedMachine.name] : [])
    if (targets.length === 0) return
    if (action === 'power-on') return openPowerConfirm('power-on', targets)
    if (action === 'power-off') return openPowerConfirm('power-off', targets)
    if (action === 'delete') return args.selectedMachines.size > 0 ? openBatchDeleteConfirm() : args.onOpenSingleDeleteConfirm()
    if (action === 'redeploy') return args.selectedMachines.size > 0 ? openBatchRedeployDialog(targets) : openRedeployDialog()
  }

  const hypervisorByHost = new Map(args.hypervisors.map((hv) => [hv.connection.host, hv]))
  const findHypervisorForMachine = (machine: Machine) => hypervisorByHost.get(machine.hostname) ?? hypervisorByHost.get(machine.ip ?? '') ?? args.hypervisors.find((hv) => hv.name === machine.name)

  function openPowerConfirm(action: BatchPowerAction, explicitTargets?: string[]) {
    const targets = explicitTargets && explicitTargets.length > 0 ? explicitTargets : Array.from(args.selectedMachines)
    if (targets.length === 0) return
    args.setBatchPowerConfirm({ open: true, action, targets: [...targets].sort(), running: false })
  }

  async function submitBatchPowerConfirm() {
    if (args.batchPowerConfirm.targets.length === 0) return
    const action = args.batchPowerConfirm.action
    const targets = [...args.batchPowerConfirm.targets]
    args.setBatchRunning(true)
    args.setBatchPowerConfirm((current) => ({ ...current, running: true }))
    try {
      const failures: string[] = []
      for (const name of targets) {
        try {
          action === 'power-on' ? await api.powerOn(name) : await api.powerOff(name)
        } catch {
          failures.push(name)
        }
      }
      args.setSelectedMachines(new Set(failures))
      if (failures.length > 0) notifyError(`Failed to ${action === 'power-on' ? 'power on' : 'power off'}: ${failures.join(', ')}`)
      args.setBatchPowerConfirm(initialBatchPowerConfirmState)
      args.onRefresh()
    } finally {
      args.setBatchRunning(false)
    }
  }

  function openBatchDeleteConfirm() {
    if (args.selectedMachines.size === 0) return
    args.setBatchDeleteConfirm({ open: true, targets: [...args.selectedMachines].sort(), running: false })
  }

  async function submitBatchDeleteConfirm() {
    if (args.batchDeleteConfirm.targets.length === 0) return
    const targets = [...args.batchDeleteConfirm.targets]
    args.setBatchRunning(true)
    args.setBatchDeleteConfirm((current) => ({ ...current, running: true }))
    try {
      const failures: string[] = []
      for (const name of targets) {
        try {
          await api.deleteMachine(name)
        } catch {
          failures.push(name)
        }
      }
      args.setSelectedMachines(new Set(failures))
      if (failures.length > 0) notifyError(`Failed to delete: ${failures.join(', ')}`)
      args.setBatchDeleteConfirm(initialBatchDeleteConfirmState)
      args.onRefresh()
    } finally {
      args.setBatchRunning(false)
    }
  }

  function updateBatchRedeployForm(target: string, updater: (current: MachineFormState) => MachineFormState) {
    args.setBatchRedeployConfirm((current) => {
      const currentForm = current.forms[target]
      if (!currentForm) return current
      return { ...current, forms: { ...current.forms, [target]: updater(currentForm) }, status: { ...current.status, [target]: { state: 'pending' } } }
    })
  }

  const machineFormReady = (formState: MachineFormState) => {
    const powerReady = formState.powerType === 'ipmi'
      ? Boolean(formState.ipmiHost.trim() && formState.ipmiUsername.trim())
      : formState.powerType === 'webhook'
        ? Boolean(formState.webhookOnURL.trim() && formState.webhookOffURL.trim())
        : true
    return Boolean(formState.hostname.trim() && formState.mac.trim() && formState.imageRef && powerReady && (!formState.subnetRef || formState.ipAssignment !== 'static' || formState.staticIP.trim()) && (formState.cloudInitMode !== 'create' || (formState.cloudInitTemplateName.trim() && formState.cloudInitUserData.trim())))
  }

  async function submitBatchRedeployConfirm() {
    const targets = [...args.batchRedeployConfirm.targets]
    if (targets.length === 0) return
    const forms = args.batchRedeployConfirm.forms
    const failures: string[] = []
    args.setBatchRunning(true)
    args.setBatchRedeployConfirm((current) => ({ ...current, running: true, status: Object.fromEntries(targets.map((target) => [target, { state: 'pending' as const }])) }))

    for (const target of targets) {
      const formState = forms[target]
      if (!formState || !machineFormReady(formState)) {
        failures.push(target)
        setRedeployStatus(target, { state: 'failed', error: 'Required redeploy fields are missing' })
        continue
      }
      setRedeployStatus(target, { state: 'running' })
      try {
        const currentMachine = args.machines.find((machine) => machine.name === target)
        const updated = await api.redeploy(target, await buildMachineSpecPayload(formState, currentMachine))
        args.onMachineUpsert(updated)
        setRedeployStatus(target, { state: 'succeeded' })
      } catch (err) {
        failures.push(target)
        setRedeployStatus(target, { state: 'failed', error: err instanceof Error ? err.message : 'Redeploy failed' })
      }
    }

    args.setBatchRunning(false)
    args.onRefresh()
    if (failures.length > 0) {
      args.setSelectedMachines(new Set(failures))
      args.setBatchRedeployConfirm((current) => ({ ...current, targets: failures, activeTarget: failures[0], running: false }))
      notifyError(`Failed to redeploy: ${failures.join(', ')}`)
      return
    }
    args.setSelectedMachines(new Set())
    args.setBatchRedeployConfirm(initialBatchRedeployConfirmState)
  }

  function toggleMachineSelect(name: string) {
    args.setSelectedMachines((prev) => {
      const next = new Set(prev)
      next.has(name) ? next.delete(name) : next.add(name)
      return next
    })
  }

  function toggleSelectAll() {
    args.setSelectedMachines(args.selectedMachines.size === args.filteredMachines.length ? new Set() : new Set(args.filteredMachines.map((machine) => machine.name)))
  }

  function toggleQuickDeploySSHKeyRef(ref: string) {
    args.setQuickDeployPreset((current) => ({ ...current, sshKeyRefs: current.sshKeyRefs.includes(ref) ? current.sshKeyRefs.filter((item) => item !== ref) : [...current.sshKeyRefs, ref] }))
  }

  function setRedeployStatus(target: string, status: { state: 'running' | 'succeeded' | 'failed'; error?: string }) {
    args.setBatchRedeployConfirm((current) => ({ ...current, activeTarget: target, status: { ...current.status, [target]: status } }))
  }

  return {
    quickDeployMachineName,
    quickDeployMachineReady,
    closeMachineDialog,
    openCreateDialog,
    submitMachineDialog,
    handleQuickDeploy,
    runPrimaryAction,
    findHypervisorForMachine,
    submitBatchPowerConfirm,
    submitBatchDeleteConfirm,
    updateBatchRedeployForm,
    machineFormReady,
    submitBatchRedeployConfirm,
    toggleMachineSelect,
    toggleSelectAll,
    toggleQuickDeploySSHKeyRef
  }
}
