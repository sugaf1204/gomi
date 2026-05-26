import type { Dispatch, RefObject, SetStateAction } from 'react'
import clsx from 'clsx'
import type { GuardedAction, MachineTab } from '../../../app-types'
import { phaseClass, powerStateClass, powerStateLabel } from '../../../lib/formatters'
import type { AuditEvent, Hypervisor, Machine, PowerConfig, Subnet } from '../../../types'
import { ActivityTab } from '../machine-tabs/ActivityTab'
import { ConfigurationTab } from '../machine-tabs/ConfigurationTab'
import { ConsoleTab } from '../machine-tabs/ConsoleTab'
import { DetailTab } from '../machine-tabs/DetailTab'
import { InfoTab } from '../machine-tabs/InfoTab'
import { MachineTabBar } from '../machine-tabs/MachineTabBar'
import { NetworkTab } from '../machine-tabs/NetworkTab'
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
}

export function MachinesWorkspace(props: MachinesWorkspaceProps) {
  return (
    <section className="min-h-0 grid grid-cols-1 md:grid-cols-[310px_minmax(0,1fr)] gap-[0.9rem]">
      <div className="min-h-0 grid grid-rows-[auto_minmax(0,1fr)] gap-[0.6rem]">
        <div className="grid gap-[0.45rem]">
          <div className="flex justify-between items-center gap-2">
            <h2 className="text-[1.4rem]">Machine List</h2>
            <div className="flex items-center justify-end flex-wrap gap-[0.35rem]">
              <button className="bg-brand border-brand-strong text-white py-[0.35rem] px-[0.55rem] text-[0.82rem]" disabled={props.quickDeploying} onClick={props.onQuickDeploy}>
                {props.quickDeploying ? 'Deploying...' : 'Quick Deploy'}
              </button>
              <button className="py-[0.35rem] px-[0.55rem] text-[0.82rem]" disabled={props.quickDeploying} onClick={props.onOpenQuickDeploySettings}>Preset</button>
              <button className="py-[0.35rem] px-[0.55rem] text-[0.82rem]" onClick={props.onOpenCreateDialog}>Add</button>
            </div>
          </div>
          <input aria-label="Machine filter" placeholder="Search name, host, MAC" value={props.machineFilter} onChange={(e) => props.onMachineFilterChange(e.target.value)} />
        </div>
        <MachineList {...props} />
      </div>
      <MachineDetailPane {...props} />
    </section>
  )
}

function MachineList({
  filteredMachines,
  dataLoading,
  selectedMachineName,
  selectedMachines,
  onSelectMachine,
  toggleMachineSelect,
  toggleSelectAll
}: MachinesWorkspaceProps) {
  return (
    <div className="overflow-auto border-t border-line pr-[0.1rem]">
      {dataLoading && filteredMachines.length === 0 && (
        <div className="grid place-items-center py-[2.4rem] text-center text-ink-soft gap-[0.55rem]">
          <div className="loading-spinner" aria-hidden="true" />
          <p className="m-0 text-[0.84rem]">Loading machines...</p>
        </div>
      )}
      {filteredMachines.length > 0 && (
        <div className="flex items-center gap-[0.45rem] py-[0.35rem] pl-[0.55rem] border-b border-line bg-[#f9f7f4]">
          <input type="checkbox" className="w-[0.95rem] h-[0.95rem] m-0 shrink-0 accent-[#2b7a78] cursor-pointer" checked={selectedMachines.size === filteredMachines.length && filteredMachines.length > 0} onChange={toggleSelectAll} />
          <span className="text-[0.78rem] text-ink-soft">Select All</span>
        </div>
      )}
      {filteredMachines.map((machine) => (
        <div key={machine.name} className={clsx('flex items-start border-0 border-b border-line border-l-[3px] border-l-transparent', selectedMachineName === machine.name && '!border-l-brand bg-[rgba(43,122,120,0.06)]')}>
          <div className="flex items-center pl-[0.55rem] pt-[0.72rem] shrink-0">
            <input type="checkbox" className="w-[0.95rem] h-[0.95rem] m-0 accent-[#2b7a78] cursor-pointer" checked={selectedMachines.has(machine.name)} onChange={() => toggleMachineSelect(machine.name)} />
          </div>
          <button className="text-left flex-1 min-w-0 border-0 bg-transparent flex justify-between items-start gap-[0.75rem] py-[0.62rem] pl-[0.45rem] pr-[0.1rem] shadow-none hover:transform-none!" onClick={() => onSelectMachine(machine.name)}>
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
  )
}

function MachineDetailPane(props: MachinesWorkspaceProps) {
  const { selectedMachine, multiSelectActive } = props
  return (
    <div className="min-h-0 grid content-start gap-[0.85rem]">
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
              <div className="absolute right-0 mt-1 min-w-[180px] bg-white border border-line shadow-[0_10px_24px_rgba(52,43,34,0.16)] z-10">
                {actions.map((item) => (
                  <button
                    key={item.value}
                    className={clsx('w-full text-left border-0 shadow-none rounded-none px-[0.7rem] py-[0.5rem]', item.value === 'power-off' || item.value === 'delete' ? 'text-[#9b2d2d] hover:bg-[#fff3f2]' : 'text-ink hover:bg-[#f7f3ed]')}
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
        <span className={clsx('text-[0.68rem] font-medium px-[0.35rem] py-[0.05rem] rounded-sm border', machine.ipAssignment === 'static' ? 'bg-[#e8f4f3] text-[#1a6360] border-[#b8dbd9]' : 'bg-[#f3f0ea] text-ink-soft border-[#e0dbd2]')}>
          {machine.ipAssignment === 'static' ? 'Static' : 'DHCP'}
        </span>
      </p>
      {machine.powerState && <p className="m-0 mt-[0.25rem]"><span className={powerStateClass(machine.powerState)}>{powerStateLabel(machine.powerState)}</span></p>}
      {hypervisor && (
        <p className="m-0 mt-[0.25rem] text-[0.82rem]">
          <span className="inline-flex items-center font-ui rounded-full text-[0.68rem] font-semibold px-2 py-0.5 bg-[#e8e0f6] text-[#5b3d8f]">Hypervisor</span>
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
