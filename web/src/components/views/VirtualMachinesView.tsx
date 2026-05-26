import { useEffect, useMemo, useRef, useState } from 'react'
import { supportsDeploymentTarget } from '../../lib/osImages'
import { usePersistentStringState } from '../../hooks/usePersistentStringState'
import type { CloudInitTemplate, Hypervisor, OSImage, SSHKey, Subnet, VirtualMachine } from '../../types'
import { VMConfigFields } from './virtual-machines/VMConfigFields'
import { VMActionDialogs } from './virtual-machines/VMActionDialogs'
import { VMEditDialogs } from './virtual-machines/VMEditDialogs'
import { VMQuickDeployDialog } from './virtual-machines/VMQuickDeployDialog'
import { VMWorkspace } from './virtual-machines/VMWorkspace'
import { useVirtualMachineOperations } from './virtual-machines/useVirtualMachineOperations'
import {
  initialBulkRedeployConfirm,
  initialDeleteConfirm,
  initialForm,
  initialMigrateConfirm,
  initialPowerConfirm,
  initialReinstallForm,
  QUICK_DEPLOY_STORAGE_KEY,
  readQuickDeployPreset,
  VM_SELECTION_STORAGE_KEY
} from './virtual-machines/vmFormState'
import type {
  QuickDeployPreset,
  VMConfigForm,
  VMBulkRedeployConfirmState,
  VMDeleteConfirmState,
  VMMigrateConfirmState,
  VMPowerConfirmState,
  VMReinstallForm,
  UpdateVMConfigForm
} from './virtual-machines/vmFormState'

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
  const [quickDeployPreset, setQuickDeployPreset] = useState<QuickDeployPreset>(readQuickDeployPreset)
  const [quickDeploySettingsOpen, setQuickDeploySettingsOpen] = useState(false)
  const [quickDeploying, setQuickDeploying] = useState(false)
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
  const vmOSImages = useMemo(() => osImages.filter((img) => supportsDeploymentTarget(img, 'vm')), [osImages])
  const checkedNames = useMemo(
    () => virtualMachines.filter((vm) => checkedVMs.has(vm.name)).map((vm) => vm.name),
    [checkedVMs, virtualMachines]
  )
  const selectedVM = virtualMachines.find((vm) => vm.name === selected) ?? null

  const {
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
  } = useVirtualMachineOperations({
    form,
    setForm,
    setFormOpen,
    setCreating,
    quickDeployPreset,
    setQuickDeployPreset,
    setQuickDeploySettingsOpen,
    setQuickDeploying,
    reinstallForm,
    setReinstallForm,
    setReinstallOpen,
    setReinstalling,
    setReinstallAdvancedOpen,
    powerConfirm,
    setPowerConfirm,
    deleteConfirm,
    setDeleteConfirm,
    bulkRedeployConfirm,
    setBulkRedeployConfirm,
    migrateConfirm,
    setMigrateConfirm,
    setAdvancedOpen,
    setCheckedVMs,
    selected,
    setSelected,
    selectedVM,
    checkedNames,
    virtualMachines,
    vmOSImages,
    osImages,
    hypervisors,
    subnets,
    onVirtualMachineUpsert,
    onRouteSelectedVMChange,
    onRefresh,
    setVMSelection,
    setConsoleVM
  })

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

  function openQuickDeploySettings() {
    setQuickDeploySettingsOpen(true)
  }

  useEffect(() => {
    if (!routeSelectedVM) return
    if (routeSelectedVM !== selected) {
      setSelected(routeSelectedVM)
    }
  }, [routeSelectedVM, selected, setSelected])

  useEffect(() => {
    if (typeof window === 'undefined') return
    try {
      const safePreset: Partial<QuickDeployPreset> = { ...quickDeployPreset }
      delete safePreset.loginUserPassword
      localStorage.setItem(QUICK_DEPLOY_STORAGE_KEY, JSON.stringify({
        ...safePreset,
        count: Math.max(1, Number(quickDeployPreset.count) || 1)
      }))
    } catch {
      // ignore localStorage access errors
    }
  }, [quickDeployPreset])

  useEffect(() => {
    if (dataLoading) return
    if (!selected) return
    if (routeSelectedVM) return
    if (!virtualMachines.some((vm) => vm.name === selected)) {
      setSelected('')
    }
  }, [dataLoading, routeSelectedVM, selected, virtualMachines, setSelected])

  useEffect(() => {
    if (!formOpen && !quickDeploySettingsOpen && !reinstallOpen && !powerConfirm.open && !deleteConfirm.open && !bulkRedeployConfirm.open && !migrateConfirm.open) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== 'Escape') return
      if (!quickDeploying) setQuickDeploySettingsOpen(false)
      if (!powerConfirm.running) setPowerConfirm(initialPowerConfirm)
      if (!reinstalling) setReinstallOpen(false)
      if (!deleteConfirm.running) setDeleteConfirm(initialDeleteConfirm)
      if (!bulkRedeployConfirm.running) setBulkRedeployConfirm(initialBulkRedeployConfirm)
      if (!migrateConfirm.running) setMigrateConfirm(initialMigrateConfirm)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [formOpen, quickDeploySettingsOpen, quickDeploying, reinstallOpen, reinstalling, powerConfirm.open, powerConfirm.running, deleteConfirm.open, deleteConfirm.running, bulkRedeployConfirm.open, bulkRedeployConfirm.running, migrateConfirm.open, migrateConfirm.running])

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

  const updateCreateConfigForm: UpdateVMConfigForm = (updater) => {
    setForm((current) => ({
      ...current,
      ...updater(current)
    }))
  }

  const updateReinstallConfigForm: UpdateVMConfigForm = (updater) => {
    setReinstallForm((current) => updater(current))
  }

  function renderVMSpecFields(
    formState: VMConfigForm,
    updateForm: UpdateVMConfigForm,
    advancedExpanded: boolean,
    setAdvancedExpanded: (value: boolean | ((current: boolean) => boolean)) => void,
    radioNamePrefix: string
  ) {
    return (
      <VMConfigFields
        formState={formState}
        updateForm={updateForm}
        advancedExpanded={advancedExpanded}
        setAdvancedExpanded={setAdvancedExpanded}
        radioNamePrefix={radioNamePrefix}
        hypervisors={hypervisors}
        vmOSImages={vmOSImages}
        cloudInits={cloudInits}
        sshKeys={sshKeys}
        subnets={subnets}
        osImageByName={osImageByName}
        onRefresh={onRefresh}
        bridgePlaceholder={bridgePlaceholder}
      />
    )
  }

  return (
    <>
      <VMQuickDeployDialog
        open={quickDeploySettingsOpen}
        preset={quickDeployPreset}
        setPreset={setQuickDeployPreset}
        quickDeploying={quickDeploying}
        hypervisors={hypervisors}
        osImages={osImages}
        cloudInits={cloudInits}
        sshKeys={sshKeys}
        subnets={subnets}
        nextName={quickDeployVMName}
        isReady={quickDeployPresetReady}
        onClose={() => setQuickDeploySettingsOpen(false)}
        onDeploy={() => void handleQuickDeploy()}
        onToggleCloudInitRef={toggleQuickDeployCloudInitRef}
        onToggleSSHKeyRef={toggleQuickDeploySSHKeyRef}
      />

      <VMEditDialogs
        formOpen={formOpen}
        creating={creating}
        form={form}
        setForm={setForm}
        advancedOpen={advancedOpen}
        setAdvancedOpen={setAdvancedOpen}
        onCloseCreate={() => setFormOpen(false)}
        onCreate={(e) => void handleCreate(e)}
        reinstallOpen={reinstallOpen}
        selectedVM={selectedVM}
        reinstalling={reinstalling}
        reinstallForm={reinstallForm}
        updateReinstallConfigForm={updateReinstallConfigForm}
        reinstallAdvancedOpen={reinstallAdvancedOpen}
        setReinstallAdvancedOpen={setReinstallAdvancedOpen}
        onCloseReinstall={() => setReinstallOpen(false)}
        onRedeploy={() => void handleRedeploy()}
        renderFields={renderVMSpecFields}
      />

      <VMActionDialogs
        deleteConfirm={deleteConfirm}
        setDeleteConfirm={setDeleteConfirm}
        onDeleteConfirm={() => void handleDeleteConfirm()}
        bulkRedeployConfirm={bulkRedeployConfirm}
        setBulkRedeployConfirm={setBulkRedeployConfirm}
        setBulkRedeployAdvancedOpen={setBulkRedeployAdvancedOpen}
        updateBulkRedeployForm={updateBulkRedeployForm}
        vmRedeployFormReady={vmRedeployFormReady}
        onBulkRedeployConfirm={() => void handleBulkRedeployConfirm()}
        powerConfirm={powerConfirm}
        setPowerConfirm={setPowerConfirm}
        onPowerConfirm={() => void handlePowerConfirm()}
        migrateConfirm={migrateConfirm}
        setMigrateConfirm={setMigrateConfirm}
        onMigrateConfirm={() => void handleMigrateConfirm()}
        virtualMachines={virtualMachines}
        hypervisors={hypervisors}
        renderFields={renderVMSpecFields}
      />

      <VMWorkspace
        virtualMachines={virtualMachines}
        dataLoading={dataLoading}
        quickDeploying={quickDeploying}
        onQuickDeploy={() => void handleQuickDeploy()}
        onOpenQuickDeploySettings={openQuickDeploySettings}
        setForm={setForm}
        setAdvancedOpen={setAdvancedOpen}
        setFormOpen={setFormOpen}
        checkedVMs={checkedVMs}
        checkedNames={checkedNames}
        selected={selected}
        selectedVM={selectedVM}
        toggleChecked={toggleChecked}
        toggleAllChecked={toggleAllChecked}
        setVMSelection={setVMSelection}
        actionsMenuRef={actionsMenuRef}
        actionsMenuOpen={actionsMenuOpen}
        setActionsMenuOpen={setActionsMenuOpen}
        runPrimaryAction={runPrimaryAction}
        consoleVM={consoleVM}
        setConsoleVM={setConsoleVM}
        formatOSImageReference={formatOSImageReference}
        formatCloudInitReferences={formatCloudInitReferences}
      />
    </>
  )
}
