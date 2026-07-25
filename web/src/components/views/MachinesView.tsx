import { useEffect, useRef, useState } from 'react'
import type { GroupBy, GuardedAction, MachineTab } from '../../app-types'
import type { AuditEvent, CloudInitTemplate, Hypervisor, Machine, PowerConfig, SSHKey, Subnet } from '../../types'
import { MachineDialogs } from './machines/MachineDialogs'
import { MachineSpecFields } from './machines/MachineSpecFields'
import { MachineConsoleDialog } from './machines/MachineConsoleDialog'
import { MachinesWorkspace } from './machines/MachinesWorkspace'
import {
  createInitialMachineForm,
  defaultSubnet,
  initialBatchDeleteConfirmState,
  initialBatchPowerConfirmState,
  initialBatchRedeployConfirmState,
  initialMachineDialogState
} from './machines/machineFormState'
import { useMachineOperations } from './machines/useMachineOperations'
import type {
  BatchDeleteConfirmState,
  BatchPowerConfirmState,
  BatchRedeployConfirmState,
  MachineFormState,
  MachineDialogState,
  UpdateMachineForm
} from './machines/machineFormState'

export type MachinesViewProps = {
  machineFilter: string
  onMachineFilterChange: (value: string) => void
  machines: Machine[]
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
  groupBy: GroupBy
  onGroupByChange: (groupBy: GroupBy) => void
  osImages: import('../../types').OSImage[]
  cloudInits: CloudInitTemplate[]
  sshKeys: SSHKey[]
}

export function MachinesView({
  machineFilter,
  onMachineFilterChange,
  machines,
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
  groupBy,
  onGroupByChange,
  osImages,
  cloudInits,
  sshKeys
}: MachinesViewProps) {
  const [machineDialog, setMachineDialog] = useState<MachineDialogState>(initialMachineDialogState)
  const [selectedMachines, setSelectedMachines] = useState<Set<string>>(new Set())
  const [consoleMachineName, setConsoleMachineName] = useState<string | null>(null)
  const multiSelectActive = selectedMachines.size > 0
  const [batchRunning, setBatchRunning] = useState(false)
  const [batchPowerConfirm, setBatchPowerConfirm] = useState<BatchPowerConfirmState>(initialBatchPowerConfirmState)
  const [batchDeleteConfirm, setBatchDeleteConfirm] = useState<BatchDeleteConfirmState>(initialBatchDeleteConfirmState)
  const [batchRedeployConfirm, setBatchRedeployConfirm] = useState<BatchRedeployConfirmState>(initialBatchRedeployConfirmState)
  const [actionsMenuOpen, setActionsMenuOpen] = useState(false)
  const actionsMenuRef = useRef<HTMLDivElement | null>(null)
  const [form, setForm] = useState<MachineFormState>(() => createInitialMachineForm(subnets))

  const {
    closeMachineDialog,
    openCreateDialog,
    submitMachineDialog,
    runPrimaryAction,
    findHypervisorForMachine,
    submitBatchPowerConfirm,
    submitBatchDeleteConfirm,
    updateBatchRedeployForm,
    machineFormReady,
    submitBatchRedeployConfirm,
    toggleMachineSelect,
    toggleSelectAll
  } = useMachineOperations({
    machineDialog,
    setMachineDialog,
    form,
    setForm,
    selectedMachines,
    setSelectedMachines,
    batchPowerConfirm,
    setBatchPowerConfirm,
    batchDeleteConfirm,
    setBatchDeleteConfirm,
    batchRedeployConfirm,
    setBatchRedeployConfirm,
    setBatchRunning,
    selectedMachine,
    machines,
    filteredMachines,
    subnets,
    hypervisors,
    onMachineUpsert,
    onSelectMachine,
    onOpenSingleDeleteConfirm: () => onOpenConfirm('delete'),
    onRefresh
  })

  useEffect(() => {
    if (!machineDialog.open && !batchPowerConfirm.open && !batchDeleteConfirm.open && !batchRedeployConfirm.open) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        if (!batchPowerConfirm.running) {
          setBatchPowerConfirm(initialBatchPowerConfirmState)
        }
        if (!batchDeleteConfirm.running) {
          setBatchDeleteConfirm(initialBatchDeleteConfirmState)
        }
        if (!batchRedeployConfirm.running) {
          setBatchRedeployConfirm(initialBatchRedeployConfirmState)
        }
        if (!machineDialog.running) {
          closeMachineDialog()
        }
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [machineDialog.open, machineDialog.running, batchPowerConfirm.open, batchPowerConfirm.running, batchDeleteConfirm.open, batchDeleteConfirm.running, batchRedeployConfirm.open, batchRedeployConfirm.running])

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

  function renderMachineSpecFields(formState: MachineFormState, updateForm: UpdateMachineForm, radioName: string) {
    return (
      <MachineSpecFields
        formState={formState}
        updateForm={updateForm}
        radioName={radioName}
        osImages={osImages}
        cloudInits={cloudInits}
        sshKeys={sshKeys}
        subnets={subnets}
        onRefresh={onRefresh}
      />
    )
  }

  return (
    <>
      <MachineDialogs
        machineDialog={machineDialog}
        closeMachineDialog={closeMachineDialog}
        selectedMachine={selectedMachine}
        form={form}
        setForm={setForm}
        submitMachineDialog={(event) => void submitMachineDialog(event)}
        renderMachineSpecFields={renderMachineSpecFields}
        batchRedeployConfirm={batchRedeployConfirm}
        setBatchRedeployConfirm={setBatchRedeployConfirm}
        updateBatchRedeployForm={updateBatchRedeployForm}
        machineFormReady={machineFormReady}
        submitBatchRedeployConfirm={() => void submitBatchRedeployConfirm()}
        batchPowerConfirm={batchPowerConfirm}
        setBatchPowerConfirm={setBatchPowerConfirm}
        submitBatchPowerConfirm={() => void submitBatchPowerConfirm()}
        batchDeleteConfirm={batchDeleteConfirm}
        setBatchDeleteConfirm={setBatchDeleteConfirm}
        submitBatchDeleteConfirm={() => void submitBatchDeleteConfirm()}
      />

      <MachineConsoleDialog
        machine={machines.find((candidate) => candidate.name === consoleMachineName) ?? null}
        onClose={() => setConsoleMachineName(null)}
      />

      <MachinesWorkspace
        machineFilter={machineFilter}
        onMachineFilterChange={onMachineFilterChange}
        filteredMachines={filteredMachines}
        dataLoading={dataLoading}
        selectedMachineName={selectedMachineName}
        onSelectMachine={onSelectMachine}
        selectedMachine={selectedMachine}
        selectedMachines={selectedMachines}
        multiSelectActive={multiSelectActive}
        onOpenCreateDialog={openCreateDialog}
        toggleMachineSelect={toggleMachineSelect}
        toggleSelectAll={toggleSelectAll}
        actionsMenuRef={actionsMenuRef}
        actionsMenuOpen={actionsMenuOpen}
        setActionsMenuOpen={setActionsMenuOpen}
        runPrimaryAction={runPrimaryAction}
        findHypervisorForMachine={findHypervisorForMachine}
        machineTab={machineTab}
        onMachineTabChange={onMachineTabChange}
        onRefresh={onRefresh}
        subnets={subnets}
        hypervisors={hypervisors}
        onOpenConsole={setConsoleMachineName}
        groupBy={groupBy}
        onGroupByChange={onGroupByChange}
        auditEvents={auditEvents}
        onSaveMachineSettings={onSaveMachineSettings}
        machineSettingsDirty={machineSettingsDirty}
        machineSettingsSaving={machineSettingsSaving}
        inlineEditField={inlineEditField}
        onInlineEditFieldChange={onInlineEditFieldChange}
        machineSettingsPower={machineSettingsPower}
        onMachineSettingsPowerChange={onMachineSettingsPowerChange}
        onOpenConfirm={onOpenConfirm}
      />
    </>
  )
}
