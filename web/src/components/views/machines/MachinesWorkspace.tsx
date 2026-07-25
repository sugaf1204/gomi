import type { Dispatch, RefObject, SetStateAction } from 'react'
import clsx from 'clsx'
import type { GroupBy, GuardedAction, MachineTab } from '../../../app-types'
import { phaseClass, powerStateClass, powerStateLabel } from '../../../lib/formatters'
import type { AuditEvent, Hypervisor, Machine, PowerConfig, Subnet } from '../../../types'
import { ActivityTab } from '../machine-tabs/ActivityTab'
import { ConfigurationTab } from '../machine-tabs/ConfigurationTab'
import { ConsoleTab } from '../machine-tabs/ConsoleTab'
import { DeployTab } from '../machine-tabs/DeployTab'
import { DetailTab } from '../machine-tabs/DetailTab'
import { InfoTab } from '../machine-tabs/InfoTab'
import { MachineTabBar } from '../machine-tabs/MachineTabBar'
import { NetworkTab } from '../machine-tabs/NetworkTab'
import { MachineListColumn } from './MachineListColumn'
import type { MachinePrimaryAction } from './machineFormState'

type MachinesWorkspaceProps = {
  machineFilter: string
  onMachineFilterChange: (value: string) => void
  filteredMachines: Machine[]
  dataLoading: boolean
  selectedMachineName: string
  onSelectMachine: (name: string) => void
  selectedMachine: Machine | null
  selectedMachines: Set<string>
  multiSelectActive: boolean
  quickDeploying: boolean
  onQuickDeploy: () => void
  onOpenQuickDeploySettings: () => void
  onOpenCreateDialog: () => void
  toggleMachineSelect: (name: string) => void
  toggleSelectAll: () => void
  actionsMenuRef: RefObject<HTMLDivElement | null>
  actionsMenuOpen: boolean
  setActionsMenuOpen: Dispatch<SetStateAction<boolean>>
  runPrimaryAction: (action: MachinePrimaryAction) => void
  findHypervisorForMachine: (machine: Machine) => Hypervisor | undefined
  machineTab: MachineTab
  onMachineTabChange: (tab: MachineTab) => void
  onRefresh: () => void | Promise<void>
  subnets: Subnet[]
  auditEvents: AuditEvent[]
  onSaveMachineSettings: () => void | Promise<void>
  machineSettingsDirty: boolean
  machineSettingsSaving: boolean
  inlineEditField: 'power' | null
  onInlineEditFieldChange: (field: 'power' | null) => void
  machineSettingsPower: PowerConfig
  onMachineSettingsPowerChange: (value: PowerConfig) => void
  onOpenConfirm: (action: GuardedAction) => void
  groupBy: GroupBy
  onGroupByChange: (groupBy: GroupBy) => void
  hypervisors: Hypervisor[]
}

export function MachinesWorkspace(props: MachinesWorkspaceProps) {
  return (
    <section className="h-full min-h-0 grid grid-cols-1 md:grid-cols-[352px_minmax(0,1fr)]">
      <MachineListColumn {...props} />
      <MachineDetailPane {...props} />
    </section>
  )
}

function MachineDetailPane(props: MachinesWorkspaceProps) {
  const { selectedMachine, multiSelectActive } = props
  return (
    <div className="min-h-0 overflow-auto grid content-start gap-[18px] p-[20px_22px]">
      {!selectedMachine && !multiSelectActive && (
        <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.45rem]">
          <h2>Select a machine</h2>
          <p>Choose a machine from the index to view actions, details, and recent operations.</p>
        </section>
      )}

      {(selectedMachine || multiSelectActive) && (
        <>
          <MachineHeader {...props} />
          {selectedMachine && <MachineTabs {...props} selectedMachine={selectedMachine} />}
        </>
      )}
    </div>
  )
}

function MachineHeader({
  selectedMachine,
  selectedMachines,
  multiSelectActive,
  actionsMenuRef,
  actionsMenuOpen,
  setActionsMenuOpen,
  runPrimaryAction,
  findHypervisorForMachine
}: MachinesWorkspaceProps) {
  const actions: Array<{ value: MachinePrimaryAction, label: string }> = [
    { value: 'power-on', label: 'Power On' },
    { value: 'power-off', label: 'Power Off' },
    { value: 'redeploy', label: 'Redeploy' },
    { value: 'delete', label: 'Delete' }
  ]

  return (
    <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid grid-cols-[minmax(0,1fr)_auto] items-start gap-[0.85rem] max-sm:grid-cols-1">
      <div className="min-w-0">
        <p className="m-0 font-ui font-medium text-[0.72rem] uppercase tracking-[0.08em] text-ink-soft">Bare Metal Machine</p>
        {multiSelectActive ? (
          <h2 className="mt-[0.22rem] text-[1.8rem] break-anywhere">{selectedMachines.size} selected</h2>
        ) : selectedMachine && (
          <SelectedMachineSummary machine={selectedMachine} hypervisor={findHypervisorForMachine(selectedMachine)} />
        )}
      </div>
      <div className="flex min-w-0 flex-col items-end gap-[0.55rem] max-sm:items-start">
        {!multiSelectActive && selectedMachine && <span className={phaseClass(selectedMachine.phase)}>{selectedMachine.phase}</span>}
        <div className="flex justify-end items-center flex-wrap gap-[0.35rem]" ref={actionsMenuRef}>
          <div className="relative">
            <button className="py-[0.45rem] px-[0.72rem]" disabled={selectedMachines.size === 0 && !selectedMachine} onClick={() => setActionsMenuOpen((current) => !current)}>Actions</button>
            {actionsMenuOpen && (
              <div className="absolute right-0 mt-1 min-w-[180px] bg-panel border border-line shadow-[0_10px_24px_rgba(52,43,34,0.16)] z-10">
                {actions.map((item) => (
                  <button
                    key={item.value}
                    className={clsx('w-full text-left border-0 shadow-none rounded-none px-[0.7rem] py-[0.5rem]', item.value === 'power-off' || item.value === 'delete' ? 'text-danger hover:bg-danger-bg' : 'text-ink hover:bg-panel-2')}
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
  )
}

function SelectedMachineSummary({ machine, hypervisor }: { machine: Machine; hypervisor?: Hypervisor }) {
  return (
    <>
      <h2 className="mt-[0.22rem] text-[1.8rem] break-anywhere">{machine.name}</h2>
      <p className="m-0 text-ink-soft break-anywhere">{machine.hostname} - {machine.arch} - {machine.firmware.toUpperCase()}</p>
      <p className="m-0 mt-[0.15rem] text-[0.84rem] flex items-center gap-[0.4rem]">
        <span className="text-ink-soft">IP:</span>
        <span>{machine.ip || '-'}</span>
        <span className={clsx('text-[0.68rem] font-medium px-[0.35rem] py-[0.05rem] rounded-sm border', machine.ipAssignment === 'static' ? 'bg-brand-wash text-brand-strong border-line' : 'bg-panel-2 text-ink-soft border-line')}>
          {machine.ipAssignment === 'static' ? 'Static' : 'DHCP'}
        </span>
      </p>
      {machine.powerState && <p className="m-0 mt-[0.25rem]"><span className={powerStateClass(machine.powerState)}>{powerStateLabel(machine.powerState)}</span></p>}
      {hypervisor && (
        <p className="m-0 mt-[0.25rem] text-[0.82rem]">
          <span className="inline-flex items-center font-ui rounded-full text-[0.68rem] font-semibold px-2 py-0.5 bg-hv-bg text-hv">Hypervisor</span>
          <span className="text-ink-soft ml-[0.4rem]">{hypervisor.vmCount} VM{hypervisor.vmCount !== 1 ? 's' : ''} hosted</span>
        </p>
      )}
    </>
  )
}

function MachineTabs(props: MachinesWorkspaceProps & { selectedMachine: Machine }) {
  const { selectedMachine, machineTab } = props
  return (
    <>
      <MachineTabBar activeTab={machineTab} onTabChange={props.onMachineTabChange} />
      {machineTab === 'info' && <InfoTab machine={selectedMachine} />}
      {machineTab === 'deploy' && <DeployTab machine={selectedMachine} />}
      {machineTab === 'detail' && <DetailTab machine={selectedMachine} />}
      {machineTab === 'network' && <NetworkTab machine={selectedMachine} subnets={props.subnets} onRefresh={props.onRefresh} />}
      {machineTab === 'console' && <ConsoleTab machine={selectedMachine} />}
      {machineTab === 'activity' && <ActivityTab auditEvents={props.auditEvents} />}
      {machineTab === 'configuration' && (
        <ConfigurationTab
          machine={selectedMachine}
          onSaveMachineSettings={props.onSaveMachineSettings}
          machineSettingsDirty={props.machineSettingsDirty}
          machineSettingsSaving={props.machineSettingsSaving}
          inlineEditField={props.inlineEditField}
          onInlineEditFieldChange={props.onInlineEditFieldChange}
          machineSettingsPower={props.machineSettingsPower}
          onMachineSettingsPowerChange={props.onMachineSettingsPowerChange}
        />
      )}
    </>
  )
}
