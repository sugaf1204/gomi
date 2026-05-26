import type { Dispatch, FormEvent, SetStateAction } from 'react'
import { api } from '../../../api'
import type { Hypervisor, OSImage, Subnet, VirtualMachine } from '../../../types'
import {
  buildAdvancedOptions,
  buildVMLoginUserPayload,
  currentVMCloudInitRefs,
  initialBulkRedeployConfirm,
  initialDeleteConfirm,
  initialForm,
  initialMigrateConfirm,
  initialPowerConfirm,
  mergeSelectedCloudInitRef,
  toReinstallForm
} from './vmFormState'
import type {
  QuickDeployPreset,
  VMBulkRedeployConfirmState,
  VMConfigForm,
  VMDeleteConfirmState,
  VMMigrateConfirmState,
  VMPowerAction,
  VMPowerConfirmState,
  VMPrimaryAction,
  VMReinstallForm,
  VMForm
} from './vmFormState'

type VMOperationsArgs = {
  form: VMForm
  setForm: Dispatch<SetStateAction<VMForm>>
  setFormOpen: Dispatch<SetStateAction<boolean>>
  setCreating: Dispatch<SetStateAction<boolean>>
  quickDeployPreset: QuickDeployPreset
  setQuickDeployPreset: Dispatch<SetStateAction<QuickDeployPreset>>
  setQuickDeploySettingsOpen: Dispatch<SetStateAction<boolean>>
  setQuickDeploying: Dispatch<SetStateAction<boolean>>
  reinstallForm: VMReinstallForm
  setReinstallForm: Dispatch<SetStateAction<VMReinstallForm>>
  setReinstallOpen: Dispatch<SetStateAction<boolean>>
  setReinstalling: Dispatch<SetStateAction<boolean>>
  setReinstallAdvancedOpen: Dispatch<SetStateAction<boolean>>
  powerConfirm: VMPowerConfirmState
  setPowerConfirm: Dispatch<SetStateAction<VMPowerConfirmState>>
  deleteConfirm: VMDeleteConfirmState
  setDeleteConfirm: Dispatch<SetStateAction<VMDeleteConfirmState>>
  bulkRedeployConfirm: VMBulkRedeployConfirmState
  setBulkRedeployConfirm: Dispatch<SetStateAction<VMBulkRedeployConfirmState>>
  migrateConfirm: VMMigrateConfirmState
  setMigrateConfirm: Dispatch<SetStateAction<VMMigrateConfirmState>>
  setAdvancedOpen: Dispatch<SetStateAction<boolean>>
  setCheckedVMs: Dispatch<SetStateAction<Set<string>>>
  selected: string
  setSelected: (value: string) => void
  selectedVM: VirtualMachine | null
  checkedNames: string[]
  virtualMachines: VirtualMachine[]
  vmOSImages: OSImage[]
  osImages: OSImage[]
  hypervisors: Hypervisor[]
  subnets: Subnet[]
  onVirtualMachineUpsert: (virtualMachine: VirtualMachine) => void
  onRouteSelectedVMChange: (value: string) => void
  onRefresh: () => void | Promise<void>
  setVMSelection: (name: string) => void
  setConsoleVM: Dispatch<SetStateAction<string | null>>
}

export function useVirtualMachineOperations(args: VMOperationsArgs) {
  const notifyError = (message: string) => {
    window.dispatchEvent(new CustomEvent('gomi:toast', { detail: { tone: 'error', message } }))
  }

  const quickDeployVMName = (preset: QuickDeployPreset) => `${preset.name.trim()}-${Math.max(1, Number(preset.count) || 1)}`
  const quickDeployPresetReady = (preset: QuickDeployPreset) => {
    if (!preset.name.trim()) return false
    if ((Number(preset.count) || 0) < 1) return false
    if (!preset.osImageRef.trim()) return false
    return args.osImages.some((img) => img.name === preset.osImageRef)
  }

  async function resolveCloudInitRefs(formState: VMConfigForm, description: string, currentRefs: string[] = []) {
    if (formState.cloudInitMode === 'none') return [] as string[]
    if (formState.cloudInitMode === 'existing') {
      return mergeSelectedCloudInitRef(formState.cloudInitExistingRef.trim(), currentRefs)
    }
    const templateName = formState.cloudInitTemplateName.trim()
    const userData = formState.cloudInitUserData.trim()
    if (!templateName || !userData) throw new Error('Cloud-Init inline creation requires both template name and user-data')
    const created = await api.createCloudInitTemplate({ name: templateName, description, userData })
    return mergeSelectedCloudInitRef(created.name, currentRefs)
  }

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (!args.form.osImageRef) return notifyError('qcow2 OS Image is required for VM deployment')
    if (!args.vmOSImages.some((img) => img.name === args.form.osImageRef)) return notifyError('VM deployment requires a vm-capable qcow2 OS Image')

    args.setCreating(true)
    try {
      const count = Math.max(1, Math.min(50, Number(args.form.count) || 1))
      const deployErrors: string[] = []
      const cloudInitRefs = await resolveCloudInitRefs(args.form, 'Auto-generated from Create Virtual Machine dialog')
      const vmNetwork = buildVMNetworkPayload(args.form)

      for (let i = 0; i < count; i++) {
        const vmName = count === 1 ? args.form.name : `${args.form.name}-${i + 1}`
        const result = await api.createVirtualMachine({
          ...buildCreateVMPayload(args.form, cloudInitRefs, vmNetwork),
          name: vmName
        })
        if (result.phase === 'Error') deployErrors.push(`${vmName}: ${result.lastError || 'deploy failed'}`)
      }

      args.setForm({ ...initialForm })
      args.setFormOpen(false)
      args.setAdvancedOpen(false)
      if (count === 1) {
        args.setVMSelection(args.form.name)
      }
      args.onRefresh()
      if (deployErrors.length > 0) notifyError(`VM created but deploy failed: ${deployErrors.join('; ')}`)
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to create VM')
    } finally {
      args.setCreating(false)
    }
  }

  async function handleQuickDeploy() {
    const preset = args.quickDeployPreset
    if (!quickDeployPresetReady(preset)) {
      args.setQuickDeploySettingsOpen(true)
      return
    }
    const vmName = quickDeployVMName(preset)
    const vmNetwork = (preset.bridge || preset.subnetRef)
      ? [{ name: 'default', bridge: preset.bridge || undefined, network: preset.subnetRef || undefined }]
      : undefined

    args.setQuickDeploying(true)
    try {
      const result = await api.createVirtualMachine({
        name: vmName,
        hypervisorRef: preset.hypervisorRef || '',
        resources: { cpuCores: Number(preset.cpuCores) || 2, memoryMB: Number(preset.memoryMB) || 2048, diskGB: Number(preset.diskGB) || 20 },
        osImageRef: preset.osImageRef,
        cloudInitRefs: preset.cloudInitRefs.length > 0 ? preset.cloudInitRefs : undefined,
        powerControlMethod: 'libvirt',
        ...(vmNetwork ? { network: vmNetwork } : {}),
        ipAssignment: preset.ipAssignment,
        ...(preset.subnetRef ? { subnetRef: preset.subnetRef } : {}),
        sshKeyRefs: preset.sshKeyRefs,
        ...(preset.loginUserUsername.trim() ? { loginUser: { username: preset.loginUserUsername.trim(), ...(preset.loginUserPassword.trim() ? { password: preset.loginUserPassword.trim() } : {}) } } : {})
      })
      args.onVirtualMachineUpsert(result)
      args.setVMSelection(vmName)
      args.setQuickDeployPreset((current) => ({ ...current, count: String(Math.max(1, Number(current.count) || 1) + 1) }))
      args.onRefresh()
      if (result.phase === 'Error') notifyError(`VM created but deploy failed: ${result.lastError || 'deploy failed'}`)
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to quick deploy VM')
    } finally {
      args.setQuickDeploying(false)
    }
  }

  const openDeleteDialog = (targets: string[]) => args.setDeleteConfirm({ open: true, targets, running: false })

  async function handleDeleteConfirm() {
    if (args.deleteConfirm.targets.length === 0) return
    const targets = [...args.deleteConfirm.targets]
    args.setDeleteConfirm(initialDeleteConfirm)
    try {
      for (const target of targets) await api.deleteVirtualMachine(target)
      if (targets.includes(args.selected)) {
        args.setSelected('')
        args.onRouteSelectedVMChange('')
      }
      args.setCheckedVMs((prev) => {
        const next = new Set(prev)
        for (const target of targets) next.delete(target)
        return next
      })
      args.onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to delete VM')
    }
  }

  const openPowerConfirm = (action: VMPowerAction, targets: string[]) => args.setPowerConfirm({ open: true, action, targets, running: false })

  async function handlePowerConfirm() {
    if (args.powerConfirm.targets.length === 0) return
    const action = args.powerConfirm.action
    const targets = [...args.powerConfirm.targets]
    args.setPowerConfirm(initialPowerConfirm)
    try {
      for (const target of targets) action === 'power-on' ? await api.vmPowerOn(target) : await api.vmPowerOff(target)
      args.onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : `Failed to ${action === 'power-on' ? 'power on' : 'power off'} VM`)
    }
  }

  function openReinstallDialog(target: VirtualMachine) {
    args.setReinstallForm(toReinstallForm(target))
    args.setReinstallAdvancedOpen(Boolean(target.advancedOptions))
    args.setReinstallOpen(true)
  }

  async function buildVMRedeployPayload(formState: VMConfigForm, description: string, currentVM?: VirtualMachine) {
    if (!formState.osImageRef) throw new Error('OS Image is required for redeploy')
    if (formState.ipAssignment === 'static' && !formState.staticIP.trim()) throw new Error('Static IP is required when static assignment is selected')
    if (!args.vmOSImages.some((img) => img.name === formState.osImageRef)) {
      notifyError('VM deployment requires a vm-capable qcow2 OS Image')
      return undefined
    }
    const cloudInitRefs = await resolveCloudInitRefs(formState, description, currentVMCloudInitRefs(currentVM))
    const vmNetwork = buildVMNetworkPayload(formState, currentVM)
    const advancedOptions = buildAdvancedOptions(formState) ?? {}
    const loginUser = buildVMLoginUserPayload(formState, currentVM?.loginUser)
    return {
      hypervisorRef: formState.hypervisorRef || '',
      resources: { cpuCores: Number(formState.cpuCores) || 2, memoryMB: Number(formState.memoryMB) || 2048, diskGB: Number(formState.diskGB) || 20 },
      osImageRef: formState.osImageRef,
      cloudInitRefs,
      ...(vmNetwork ? { network: vmNetwork } : {}),
      ...(formState.subnetRef ? { subnetRef: formState.subnetRef } : { subnetRef: '' }),
      domain: formState.domain.trim() || undefined,
      advancedOptions,
      ipAssignment: formState.ipAssignment,
      ...(formState.ipAssignment === 'static' && formState.staticIP ? { ip: formState.staticIP } : { ip: '' }),
      sshKeyRefs: formState.sshKeyRefs,
      ...(loginUser ? { loginUser } : {})
    } satisfies Parameters<typeof api.vmRedeploy>[1]
  }

  async function handleRedeploy() {
    if (!args.selectedVM) return
    args.setReinstalling(true)
    try {
      const payload = await buildVMRedeployPayload(args.reinstallForm, 'Auto-generated from Redeploy Virtual Machine dialog', args.selectedVM)
      if (!payload) return
      const updated = await api.vmRedeploy(args.selectedVM.name, payload)
      args.onVirtualMachineUpsert(updated)
      args.setReinstallOpen(false)
      args.onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to redeploy VM')
    } finally {
      args.setReinstalling(false)
    }
  }

  function openBulkRedeployDialog(targets: string[]) {
    const sortedTargets = [...targets].sort()
    const forms: Record<string, VMReinstallForm> = {}
    const advancedOpen: Record<string, boolean> = {}
    const status: VMBulkRedeployConfirmState['status'] = {}
    for (const target of sortedTargets) {
      const vm = args.virtualMachines.find((item) => item.name === target)
      if (!vm) continue
      forms[target] = toReinstallForm(vm)
      advancedOpen[target] = Boolean(vm.advancedOptions)
      status[target] = { state: 'pending' }
    }
    const availableTargets = sortedTargets.filter((target) => forms[target])
    if (availableTargets.length === 0) return
    args.setBulkRedeployConfirm({ open: true, targets: availableTargets, activeTarget: availableTargets[0], forms, advancedOpen, status, running: false })
  }

  function updateBulkRedeployForm(target: string, updater: (current: VMConfigForm) => VMConfigForm) {
    args.setBulkRedeployConfirm((current) => {
      const currentForm = current.forms[target]
      if (!currentForm) return current
      return { ...current, forms: { ...current.forms, [target]: updater(currentForm) }, status: { ...current.status, [target]: { state: 'pending' } } }
    })
  }

  function setBulkRedeployAdvancedOpen(target: string, value: boolean | ((current: boolean) => boolean)) {
    args.setBulkRedeployConfirm((current) => {
      const currentValue = current.advancedOpen[target] ?? false
      return { ...current, advancedOpen: { ...current.advancedOpen, [target]: typeof value === 'function' ? value(currentValue) : value } }
    })
  }

  const vmRedeployFormReady = (formState: VMConfigForm) => {
    const cpuCores = Number(formState.cpuCores)
    const memoryMB = Number(formState.memoryMB)
    const diskGB = Number(formState.diskGB)
    return Boolean(formState.osImageRef && Number.isFinite(cpuCores) && cpuCores >= 1 && Number.isFinite(memoryMB) && memoryMB > 0 && Number.isFinite(diskGB) && diskGB >= 1 && (formState.ipAssignment !== 'static' || formState.staticIP.trim()) && (formState.cloudInitMode !== 'create' || (formState.cloudInitTemplateName.trim() && formState.cloudInitUserData.trim())))
  }

  async function handleBulkRedeployConfirm() {
    const targets = [...args.bulkRedeployConfirm.targets]
    if (targets.length === 0) return
    const forms = args.bulkRedeployConfirm.forms
    const failures: string[] = []
    args.setBulkRedeployConfirm((current) => ({ ...current, running: true, status: Object.fromEntries(targets.map((target) => [target, { state: 'pending' as const }])) }))

    for (const target of targets) {
      const formState = forms[target]
      if (!formState || !vmRedeployFormReady(formState)) {
        failures.push(target)
        setRedeployTargetFailed(target, 'Required redeploy fields are missing')
        continue
      }
      setRedeployTargetRunning(target)
      try {
        const currentVM = args.virtualMachines.find((vm) => vm.name === target)
        const payload = await buildVMRedeployPayload(formState, 'Auto-generated from Bulk Redeploy Virtual Machine dialog', currentVM)
        if (!payload) throw new Error('Invalid VM redeploy payload')
        args.onVirtualMachineUpsert(await api.vmRedeploy(target, payload))
        setRedeployTargetSucceeded(target)
      } catch (err) {
        failures.push(target)
        setRedeployTargetFailed(target, err instanceof Error ? err.message : 'Redeploy failed')
      }
    }

    args.onRefresh()
    if (failures.length > 0) {
      args.setCheckedVMs(new Set(failures))
      args.setBulkRedeployConfirm((current) => ({ ...current, targets: failures, activeTarget: failures[0], running: false }))
      notifyError(`Failed to redeploy VM: ${failures.join(', ')}`)
      return
    }
    args.setCheckedVMs(new Set())
    args.setBulkRedeployConfirm(initialBulkRedeployConfirm)
  }

  async function handleMigrateConfirm() {
    if (!args.migrateConfirm.vmName) return
    args.setMigrateConfirm((prev) => ({ ...prev, running: true }))
    try {
      await api.vmMigrate(args.migrateConfirm.vmName, args.migrateConfirm.targetHypervisor ? { targetHypervisor: args.migrateConfirm.targetHypervisor } : {})
      args.setMigrateConfirm(initialMigrateConfirm)
      args.onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to migrate VM')
      args.setMigrateConfirm((prev) => ({ ...prev, running: false }))
    }
  }

  function runPrimaryAction(action: VMPrimaryAction) {
    const targets = args.checkedNames.length > 0 ? args.checkedNames : (args.selectedVM ? [args.selectedVM.name] : [])
    if (targets.length === 0) return
    if (action === 'console' && args.selectedVM && (args.selectedVM.phase === 'Running' || args.selectedVM.phase === 'Provisioning')) return args.setConsoleVM(args.selectedVM.name)
    if (action === 'power-on') return openPowerConfirm('power-on', targets)
    if (action === 'power-off') return openPowerConfirm('power-off', targets)
    if (action === 'delete') return openDeleteDialog(targets)
    if (action === 'migrate' && args.selectedVM && args.selectedVM.phase === 'Running') return args.setMigrateConfirm({ open: true, vmName: args.selectedVM.name, targetHypervisor: '', running: false })
    if (action === 'redeploy') args.checkedNames.length > 0 ? openBulkRedeployDialog(targets) : args.selectedVM && openReinstallDialog(args.selectedVM)
  }

  function toggleQuickDeployCloudInitRef(ref: string) {
    args.setQuickDeployPreset((current) => ({ ...current, cloudInitRefs: toggleRef(current.cloudInitRefs, ref) }))
  }

  function toggleQuickDeploySSHKeyRef(ref: string) {
    args.setQuickDeployPreset((current) => ({ ...current, sshKeyRefs: toggleRef(current.sshKeyRefs, ref) }))
  }

  function bridgePlaceholder(formState: VMConfigForm): string {
    const hypervisor = args.hypervisors.find((item) => item.name === formState.hypervisorRef)
    if (hypervisor?.bridgeName) return hypervisor.bridgeName
    if (formState.subnetRef) return args.subnets.find((item) => item.name === formState.subnetRef)?.spec.pxeInterface || 'virbr0'
    return 'virbr0'
  }

  const setRedeployTargetRunning = (target: string) => setRedeployTargetStatus(target, { state: 'running' })
  const setRedeployTargetSucceeded = (target: string) => setRedeployTargetStatus(target, { state: 'succeeded' })
  const setRedeployTargetFailed = (target: string, error: string) => setRedeployTargetStatus(target, { state: 'failed', error })
  const setRedeployTargetStatus = (target: string, status: { state: 'running' | 'succeeded' | 'failed'; error?: string }) => {
    args.setBulkRedeployConfirm((current) => ({ ...current, activeTarget: target, status: { ...current.status, [target]: status } }))
  }

  return {
    quickDeployVMName,
    quickDeployPresetReady,
    handleCreate,
    handleQuickDeploy,
    handleDeleteConfirm,
    handlePowerConfirm,
    handleRedeploy,
    updateBulkRedeployForm,
    setBulkRedeployAdvancedOpen,
    vmRedeployFormReady,
    handleBulkRedeployConfirm,
    handleMigrateConfirm,
    runPrimaryAction,
    toggleQuickDeployCloudInitRef,
    toggleQuickDeploySSHKeyRef,
    bridgePlaceholder
  }
}

function buildVMNetworkPayload(formState: VMConfigForm, currentVM?: VirtualMachine): VirtualMachine['network'] | undefined {
  const primaryNetwork = {
    ...(currentVM?.network?.[0] ?? {}),
    name: currentVM?.network?.[0]?.name || 'default',
    bridge: formState.bridge || undefined,
    network: formState.subnetRef || undefined,
    ipAddress: formState.ipAssignment === 'static' && formState.staticIP ? formState.staticIP : undefined
  }
  if (currentVM?.network && currentVM.network.length > 0) return [primaryNetwork, ...currentVM.network.slice(1)]
  if (formState.bridge || formState.staticIP || formState.subnetRef) return [primaryNetwork]
  return undefined
}

function buildCreateVMPayload(form: VMForm, cloudInitRefs: string[], vmNetwork: VirtualMachine['network'] | undefined) {
  const advancedOptions = buildAdvancedOptions(form)
  return {
    hypervisorRef: form.hypervisorRef || '',
    resources: { cpuCores: Number(form.cpuCores) || 2, memoryMB: Number(form.memoryMB) || 2048, diskGB: Number(form.diskGB) || 20 },
    osImageRef: form.osImageRef,
    cloudInitRefs: cloudInitRefs.length > 0 ? cloudInitRefs : undefined,
    powerControlMethod: 'libvirt' as const,
    ...(advancedOptions ? { advancedOptions } : {}),
    ...(vmNetwork ? { network: vmNetwork } : {}),
    ...(form.ipAssignment === 'static' ? { ipAssignment: 'static' as const } : {}),
    ...(form.subnetRef ? { subnetRef: form.subnetRef } : {}),
    ...(form.domain.trim() ? { domain: form.domain.trim() } : {}),
    sshKeyRefs: form.sshKeyRefs,
    ...(form.loginUserUsername.trim() ? { loginUser: { username: form.loginUserUsername.trim(), ...(form.loginUserPassword.trim() ? { password: form.loginUserPassword.trim() } : {}) } } : {})
  }
}

function toggleRef(refs: string[], ref: string) {
  return refs.includes(ref) ? refs.filter((item) => item !== ref) : [...refs, ref]
}
