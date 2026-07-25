import clsx from 'clsx'
import { groupVirtualMachines, vmCountLabel } from '../../../lib/grouping'
import { phaseClass } from '../../../lib/formatters'
import type { Hypervisor, VirtualMachine } from '../../../types'

export type VMListColumnProps = {
  vmFilter: string
  onVMFilterChange: (value: string) => void
  filteredVMs: VirtualMachine[]
  hypervisors: Hypervisor[]
  dataLoading: boolean
  checkedVMs: Set<string>
  selected: string
  toggleChecked: (name: string) => void
  toggleAllChecked: () => void
  setVMSelection: (name: string) => void
  onOpenCreateDialog: () => void
}

export function VMListColumn(props: VMListColumnProps) {
  const groups = groupVirtualMachines(props.filteredVMs, props.hypervisors)

  return (
    <div className="min-h-0 grid grid-rows-[auto_auto_minmax(0,1fr)] border-r border-line bg-panel-2">
      <Toolbar {...props} />
      <SelectAllRow {...props} />

      <div className="overflow-auto">
        {props.dataLoading && props.filteredVMs.length === 0 && (
          <p className="m-0 py-6 text-center font-mono text-[11.5px] text-ink-soft">Loading virtual machines…</p>
        )}
        {groups.map((group) => (
          <section key={group.key}>
            <header className="flex items-baseline gap-2 py-2 px-[14px] bg-panel-3 border-t border-line">
              <span className="font-mono font-semibold text-[11px] truncate">{group.title}</span>
              {group.facts && <span className="font-mono text-[10px] text-ink-soft truncate">{group.facts}</span>}
              <span className="ml-auto font-mono font-medium text-[10px] text-ink-soft shrink-0">
                {vmCountLabel(group.virtualMachines.length)}
              </span>
            </header>
            {group.virtualMachines.map((vm) => (
              <VMRow key={vm.name} vm={vm} {...props} />
            ))}
          </section>
        ))}
        {!props.dataLoading && props.filteredVMs.length === 0 && (
          <p className="m-0 py-6 text-center font-mono text-[11.5px] text-ink-soft">No virtual machines found</p>
        )}
      </div>
    </div>
  )
}

function Toolbar({
  vmFilter,
  onVMFilterChange,
  onOpenCreateDialog,
}: VMListColumnProps) {
  return (
    <div className="flex items-center gap-2 pt-3 px-[14px] pb-[10px]">
      <input
        aria-label="Virtual machine filter"
        placeholder="filter name, hypervisor"
        value={vmFilter}
        onChange={(event) => onVMFilterChange(event.target.value)}
        className="flex-1 min-w-0 border border-line py-[6px] px-2 font-mono text-[11.5px]"
      />
      <button className="shrink-0 py-[6px] px-2 text-[11.5px]" onClick={onOpenCreateDialog}>Add</button>
    </div>
  )
}

function SelectAllRow({ filteredVMs, checkedVMs, toggleAllChecked }: VMListColumnProps) {
  if (filteredVMs.length === 0) return null
  return (
    <div className="flex items-center gap-2 py-[6px] pl-[14px] border-b border-line">
      <input
        type="checkbox"
        aria-label="Select all virtual machines"
        className="w-[0.95rem] h-[0.95rem] m-0 shrink-0 accent-brand cursor-pointer"
        checked={checkedVMs.size === filteredVMs.length}
        onChange={toggleAllChecked}
      />
      <span className="font-mono text-[10.5px] text-ink-soft">select all</span>
    </div>
  )
}

/**
 * Share of the provisioning window that has already elapsed, from the server's
 * own startedAt/deadlineAt pair. Returns null when either timestamp is missing
 * so the caller can fall back rather than render an invented fraction.
 */
export function provisioningElapsedShare(vm: VirtualMachine, nowMs: number): number | null {
  const started = Date.parse(vm.provisioning?.startedAt ?? '')
  const deadline = Date.parse(vm.provisioning?.deadlineAt ?? '')
  if (Number.isNaN(started) || Number.isNaN(deadline) || deadline <= started) return null
  return Math.min(1, Math.max(0, (nowMs - started) / (deadline - started)))
}

/**
 * Static two-tone line for a VM that is still being created: the elapsed share
 * of the provisioning window is warm, the remainder is track. It is a tone
 * split rendered once per data update, never an animation.
 */
function ProvisioningLine({ vm }: { vm: VirtualMachine }) {
  const share = provisioningElapsedShare(vm, Date.now())
  const percent = share === null ? 50 : share * 100
  return (
    <span
      role="img"
      aria-label={share === null ? 'creating, elapsed time unknown' : `creating, ${Math.round(percent)}% of window elapsed`}
      className="mt-[4px] flex h-[5px] border border-line-soft"
    >
      <span aria-hidden="true" className="bg-warn-bg" style={{ width: `${percent}%` }} />
      <span aria-hidden="true" className="flex-1 bg-track" />
    </span>
  )
}

/** Left sub-line: placement and shape of the VM, or its error when it has one. */
function subLine(vm: VirtualMachine): string {
  const placement = vm.hypervisorRef || vm.hypervisorName || 'auto-placed'
  return `${placement} · ${vm.resources.cpuCores}c · ${Math.round(vm.resources.memoryMB / 1024)}g`
}

/** Right sub-line: the single most useful runtime fact under the phase chip. */
function statusLine(vm: VirtualMachine): string {
  if ((vm.phase === 'Error' || vm.phase === 'Missing') && vm.lastError) return vm.lastError
  const ip = vm.ipAddresses?.[0]
  if (ip) return ip
  return vm.libvirtDomain || ''
}

function VMRow({ vm, selected, setVMSelection, checkedVMs, toggleChecked }: VMListColumnProps & { vm: VirtualMachine }) {
  const isSelected = selected === vm.name
  const creating = vm.phase === 'Creating' || vm.phase === 'Provisioning'
  const status = statusLine(vm)

  return (
    <div
      className={clsx(
        'grid grid-cols-[3px_auto_minmax(0,1fr)_auto] items-start border-b border-line-soft',
        isSelected ? 'bg-brand-wash' : 'bg-panel'
      )}
    >
      <span aria-hidden="true" className={clsx('self-stretch', isSelected ? 'bg-brand' : 'bg-transparent')} />
      <div className="flex items-center pl-2 pt-[10px]">
        <input
          type="checkbox"
          aria-label={`Select ${vm.name}`}
          className="w-[0.95rem] h-[0.95rem] m-0 accent-brand cursor-pointer"
          checked={checkedVMs.has(vm.name)}
          onChange={() => toggleChecked(vm.name)}
        />
      </div>
      <button
        className="text-left min-w-0 border-0 bg-transparent shadow-none py-[9px] pl-[11px] pr-1 hover:transform-none! hover:shadow-none!"
        onClick={() => setVMSelection(vm.name)}
      >
        <span className="block font-mono font-medium text-[12.5px] truncate">{vm.name}</span>
        {creating ? (
          <ProvisioningLine vm={vm} />
        ) : (
          <span className="block font-mono text-[10.5px] text-ink-soft truncate">{subLine(vm)}</span>
        )}
      </button>
      <div className="flex flex-col items-end gap-[2px] py-[9px] pl-2 pr-3 shrink-0 max-w-[38%]">
        <span className={phaseClass(vm.phase)}>{vm.phase}</span>
        {status && <span className="font-mono text-[10.5px] text-ink-soft truncate max-w-full">{status}</span>}
      </div>
    </div>
  )
}
