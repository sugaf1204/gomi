import type { Dispatch, RefObject, SetStateAction } from 'react'
import clsx from 'clsx'
import type { GroupBy, GuardedAction, MachineTab } from '../../../app-types'
import { phaseClass, powerStateClass, powerStateLabel } from '../../../lib/formatters'
import type { AuditEvent, Hypervisor, Machine, PowerConfig, Subnet } from '../../../types'
import { ConfigurationTab } from '../machine-tabs/ConfigurationTab'
import { DeployTab } from '../machine-tabs/DeployTab'
import { MachineTabBar } from '../machine-tabs/MachineTabBar'
import { NetworkTab } from '../machine-tabs/NetworkTab'
import { OverviewTab } from '../machine-tabs/OverviewTab'
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
  onOpenConsole: (name: string) => void
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
  findHypervisorForMachine,
  subnets,
  onOpenConsole
}: MachinesWorkspaceProps) {
  const actions: Array<{ value: MachinePrimaryAction, label: string }> = [
    { value: 'power-on', label: 'Power On' },
    { value: 'power-off', label: 'Power Off' },
    { value: 'redeploy', label: 'Redeploy' },
    { value: 'delete', label: 'Delete' }
  ]

  return (
    <section className="bg-transparent border-0 shadow-none grid grid-cols-[minmax(0,1fr)_auto] items-start gap-[0.85rem] max-sm:grid-cols-1">
      <div className="min-w-0">
        <p className="m-0 font-mono text-[10px] uppercase tracking-[0.14em] text-ink-soft">
          {selectedMachine ? kicker(selectedMachine, subnets) : 'BARE METAL'}
        </p>
        {multiSelectActive ? (
          <h2 className="mt-2 font-mono font-medium text-[30px] tracking-[-0.02em] break-anywhere">{selectedMachines.size} selected</h2>
        ) : selectedMachine && (
          <SelectedMachineSummary machine={selectedMachine} hypervisor={findHypervisorForMachine(selectedMachine)} />
        )}
      </div>
      <div className="flex min-w-0 flex-col items-end gap-[0.55rem] max-sm:items-start">
        {!multiSelectActive && selectedMachine && <span className={phaseClass(selectedMachine.phase)}>{selectedMachine.phase}</span>}
        <div className="flex justify-end items-center flex-wrap gap-[6px]" ref={actionsMenuRef}>
          {!multiSelectActive && selectedMachine && (
            <>
              <button className="py-[7px] px-[11px] text-[12px] font-medium" onClick={() => onOpenConsole(selectedMachine.name)}>Console</button>
              <button className="py-[7px] px-[11px] text-[12px] font-medium" onClick={() => runPrimaryAction('redeploy')}>Redeploy</button>
            </>
          )}
          <div className="relative">
            <button className="bg-brand border-brand-strong text-white py-[7px] px-[11px] text-[12px] font-medium" disabled={selectedMachines.size === 0 && !selectedMachine} onClick={() => setActionsMenuOpen((current) => !current)}>Actions ▾</button>
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

// "BARE METAL · SUBNET default · 10.0.0.0/24" — where this machine sits.
function kicker(machine: Machine, subnets: Subnet[]): string {
  const subnet = subnets.find((candidate) => candidate.name === machine.subnetRef)
  if (!subnet) return 'BARE METAL'
  return `BARE METAL · SUBNET ${subnet.name} · ${subnet.spec.cidr}`
}

function SelectedMachineSummary({ machine, hypervisor }: { machine: Machine; hypervisor?: Hypervisor }) {
  return (
    <>
      <h2 className="mt-2 font-mono font-medium text-[30px] tracking-[-0.02em] break-anywhere">{machine.name}</h2>
      <p className="m-0 mt-1 text-[12px] text-ink-soft flex items-center flex-wrap gap-2 break-anywhere">
        <span className="font-mono">{machine.hostname} · {machine.arch} · {machine.firmware.toUpperCase()} · {machine.ip || '—'}</span>
        <span className="shrink-0 font-mono text-[10.5px] border border-ok-line bg-brand-wash text-brand-accent py-[2px] px-[5px]">
          {machine.ipAssignment === 'static' ? 'STATIC' : 'DHCP'}
        </span>
        {machine.powerState && <span className={powerStateClass(machine.powerState)}>{powerStateLabel(machine.powerState)}</span>}
        {hypervisor && (
          <span className="shrink-0 font-mono font-semibold text-[10px] tracking-[0.08em] py-[3px] px-1 bg-hv-bg text-hv">
            HV · {hypervisor.vmCount} VM{hypervisor.vmCount !== 1 ? 's' : ''}
          </span>
        )}
      </p>
    </>
  )
}

function MachineTabs(props: MachinesWorkspaceProps & { selectedMachine: Machine }) {
  const { selectedMachine, machineTab } = props
  return (
    <>
      <MachineTabBar activeTab={machineTab} onTabChange={props.onMachineTabChange} />
      {machineTab === 'overview' && <OverviewTab machine={selectedMachine} subnets={props.subnets} />}
      {machineTab === 'deploy' && <DeployTab machine={selectedMachine} auditEvents={props.auditEvents} />}
      {machineTab === 'config' && (
        <div className="grid gap-[18px] pt-[14px]">
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
          <NetworkTab machine={selectedMachine} subnets={props.subnets} onRefresh={props.onRefresh} />
        </div>
      )}
    </>
  )
}
