import clsx from 'clsx'
import type { GroupBy } from '../../../app-types'
import { groupCountLabel, groupMachines } from '../../../lib/grouping'
import { phaseClass } from '../../../lib/formatters'
import type { Hypervisor, Machine, Subnet } from '../../../types'

export type MachineListColumnProps = {
  machineFilter: string
  onMachineFilterChange: (value: string) => void
  filteredMachines: Machine[]
  dataLoading: boolean
  selectedMachineName: string
  onSelectMachine: (name: string) => void
  selectedMachines: Set<string>
  toggleMachineSelect: (name: string) => void
  toggleSelectAll: () => void
  onOpenCreateDialog: () => void
  groupBy: GroupBy
  onGroupByChange: (groupBy: GroupBy) => void
  subnets: Subnet[]
  hypervisors: Hypervisor[]
  findHypervisorForMachine: (machine: Machine) => Hypervisor | undefined
}

const GROUP_OPTIONS: GroupBy[] = ['subnet', 'hypervisor', 'phase']

export function MachineListColumn(props: MachineListColumnProps) {
  const groups = groupMachines(props.filteredMachines, props.groupBy, {
    subnets: props.subnets,
    hypervisors: props.hypervisors,
  })

  return (
    <div className="min-h-0 grid grid-rows-[auto_auto_auto_minmax(0,1fr)] border-r border-line bg-panel-2">
      <Toolbar {...props} />
      <GroupSwitch groupBy={props.groupBy} onGroupByChange={props.onGroupByChange} />
      <SelectAllRow {...props} />

      <div className="overflow-auto">
        {props.dataLoading && props.filteredMachines.length === 0 && (
          <p className="m-0 py-6 text-center font-mono text-[11.5px] text-ink-soft">Loading machines…</p>
        )}
        {groups.map((group) => (
          <section key={group.key}>
            <header className="flex items-baseline gap-2 py-2 px-[14px] bg-panel-3 border-t border-line">
              <span className="font-mono font-semibold text-[11px] truncate">{group.title}</span>
              {group.facts && <span className="font-mono text-[10px] text-ink-soft truncate">{group.facts}</span>}
              <span className="ml-auto font-mono font-medium text-[10px] text-ink-soft shrink-0">
                {groupCountLabel(group.machines.length)}
              </span>
            </header>
            {group.machines.map((machine) => (
              <MachineRow key={machine.name} machine={machine} {...props} />
            ))}
          </section>
        ))}
        {!props.dataLoading && props.filteredMachines.length === 0 && (
          <p className="m-0 py-6 text-center font-mono text-[11.5px] text-ink-soft">No machines found</p>
        )}
      </div>
    </div>
  )
}

function Toolbar({
  machineFilter,
  onMachineFilterChange,
  onOpenCreateDialog,
}: MachineListColumnProps) {
  return (
    <div className="flex items-center gap-2 pt-3 px-[14px] pb-2">
      <input
        aria-label="Machine filter"
        placeholder="filter name, host, mac"
        value={machineFilter}
        onChange={(event) => onMachineFilterChange(event.target.value)}
        className="flex-1 min-w-0 border border-line py-[6px] px-2 font-mono text-[11.5px]"
      />
      <button className="shrink-0 bg-brand border-brand-strong text-white py-[6px] px-[10px] text-[11.5px] font-medium" onClick={onOpenCreateDialog}>
        Add Machine
      </button>
    </div>
  )
}

function GroupSwitch({ groupBy, onGroupByChange }: Pick<MachineListColumnProps, 'groupBy' | 'onGroupByChange'>) {
  return (
    <div className="flex items-center gap-2 px-[14px] pb-[10px]">
      <span className="font-mono text-[10.5px] tracking-[0.1em] text-ink-soft">GROUP</span>
      <div className="flex border border-line-soft" role="group" aria-label="Group machines by">
        {GROUP_OPTIONS.map((option) => (
          <button
            key={option}
            type="button"
            aria-pressed={groupBy === option}
            onClick={() => onGroupByChange(option)}
            className={clsx(
              'border-0 shadow-none py-1 px-2 font-mono text-[10px] hover:transform-none! hover:shadow-none!',
              groupBy === option ? 'bg-bg-soft text-ink' : 'bg-transparent text-ink-soft'
            )}
          >
            {option}
          </button>
        ))}
      </div>
    </div>
  )
}

function SelectAllRow({ filteredMachines, selectedMachines, toggleSelectAll }: MachineListColumnProps) {
  if (filteredMachines.length === 0) return null
  return (
    <div className="flex items-center gap-2 py-[6px] pl-[14px] border-b border-line">
      <input
        type="checkbox"
        aria-label="Select all machines"
        className="w-[0.95rem] h-[0.95rem] m-0 shrink-0 accent-brand cursor-pointer"
        checked={selectedMachines.size === filteredMachines.length}
        onChange={toggleSelectAll}
      />
      <span className="font-mono text-[10.5px] text-ink-soft">select all</span>
    </div>
  )
}

/** Right-hand sub-line: what this machine is doing right now. */
function powerLine(machine: Machine, hypervisor?: Hypervisor): string {
  if (hypervisor) return `${hypervisor.vmCount} VM${hypervisor.vmCount === 1 ? '' : 's'} hosted`
  if (machine.powerState === 'running') return 'powered on'
  if (machine.powerState === 'stopped') return 'powered off'
  return ''
}

function MachineRow({
  machine,
  selectedMachineName,
  onSelectMachine,
  selectedMachines,
  toggleMachineSelect,
  findHypervisorForMachine,
}: MachineListColumnProps & { machine: Machine }) {
  const selected = selectedMachineName === machine.name
  const hypervisor = findHypervisorForMachine(machine)
  const sub = powerLine(machine, hypervisor)

  return (
    <div
      className={clsx(
        'grid grid-cols-[3px_auto_minmax(0,1fr)_auto] items-start border-b border-line-soft',
        selected ? 'bg-brand-wash' : 'bg-panel'
      )}
    >
      <span aria-hidden="true" className={clsx('self-stretch', selected ? 'bg-brand' : 'bg-transparent')} />
      <div className="flex items-center pl-2 pt-[10px]">
        <input
          type="checkbox"
          aria-label={`Select ${machine.name}`}
          className="w-[0.95rem] h-[0.95rem] m-0 accent-brand cursor-pointer"
          checked={selectedMachines.has(machine.name)}
          onChange={() => toggleMachineSelect(machine.name)}
        />
      </div>
      <button
        className="text-left min-w-0 border-0 bg-transparent shadow-none py-[9px] pl-[11px] pr-1 hover:transform-none! hover:shadow-none!"
        onClick={() => onSelectMachine(machine.name)}
      >
        <span className="flex items-center gap-[6px] min-w-0">
          <span className="font-mono font-medium text-[12.5px] truncate">{machine.name}</span>
          {machine.role === 'hypervisor' && (
            <span className="shrink-0 font-mono font-semibold text-[10px] tracking-[0.08em] py-[3px] px-1 bg-hv-bg text-hv">HV</span>
          )}
        </span>
        <span className="block font-mono text-[10.5px] text-ink-soft truncate">
          {machine.osPreset.family} {machine.osPreset.version} · {machine.ip || machine.mac}
        </span>
      </button>
      <div className="flex flex-col items-end gap-[2px] py-[9px] pl-2 pr-3 shrink-0">
        <span className={phaseClass(machine.phase)}>{machine.phase}</span>
        {sub && <span className="font-mono text-[10.5px] text-ink-soft whitespace-nowrap">{sub}</span>}
      </div>
    </div>
  )
}
