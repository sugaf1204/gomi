import type { Dispatch, ReactNode, RefObject, SetStateAction } from 'react'
import clsx from 'clsx'
import { formatDate, phaseClass } from '../../../lib/formatters'
import type { Hypervisor, VirtualMachine } from '../../../types'
import { VMConsolePanel } from '../VMConsolePanel'
import { VMListColumn } from './VMListColumn'
import { VMResourceShareCard } from './VMResourceShareCard'
import type { VMPrimaryAction } from './vmFormState'

type VMWorkspaceProps = {
  virtualMachines: VirtualMachine[]
  filteredVMs: VirtualMachine[]
  vmFilter: string
  onVMFilterChange: (value: string) => void
  hypervisors: Hypervisor[]
  dataLoading: boolean
  quickDeploying: boolean
  onQuickDeploy: () => void
  onOpenQuickDeploySettings: () => void
  onOpenCreateDialog: () => void
  checkedVMs: Set<string>
  checkedNames: string[]
  selected: string
  selectedVM: VirtualMachine | null
  toggleChecked: (name: string) => void
  toggleAllChecked: () => void
  setVMSelection: (name: string) => void
  actionsMenuRef: RefObject<HTMLDivElement | null>
  actionsMenuOpen: boolean
  setActionsMenuOpen: Dispatch<SetStateAction<boolean>>
  runPrimaryAction: (action: VMPrimaryAction) => void
  consoleVM: string | null
  setConsoleVM: Dispatch<SetStateAction<string | null>>
  formatOSImageReference: (ref?: string) => string
  formatCloudInitReferences: (vm: VirtualMachine) => string
}

export function VMWorkspace(props: VMWorkspaceProps) {
  return (
    <section className="h-full min-h-0 grid grid-cols-1 md:grid-cols-[352px_minmax(0,1fr)]">
      <VMListColumn {...props} />
      <VMDetailPane {...props} />
    </section>
  )
}

/** The hypervisor this VM actually runs on, preferring the requested ref. */
function hostOf(vm: VirtualMachine, hypervisors: Hypervisor[]): Hypervisor | undefined {
  const ref = vm.hypervisorRef || vm.hypervisorName || ''
  if (!ref) return undefined
  return hypervisors.find((hypervisor) => hypervisor.name === ref)
}

function VMDetailPane(props: VMWorkspaceProps) {
  const { selectedVM, checkedNames, virtualMachines, consoleVM, setConsoleVM } = props
  const consoleTarget = consoleVM ? virtualMachines.find((vm) => vm.name === consoleVM) : undefined

  return (
    <div className="min-h-0 overflow-auto grid content-start gap-[18px] p-[20px_22px]">
      {!selectedVM && checkedNames.length === 0 && (
        <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.45rem]">
          <h2>Select a virtual machine</h2>
          <p>Choose a VM from the index to view actions, details, and resource usage.</p>
        </section>
      )}

      {(selectedVM || checkedNames.length > 0) && <VMHeader {...props} />}

      {consoleTarget && <VMConsolePanel vm={consoleTarget} onClose={() => setConsoleVM(null)} />}

      {selectedVM && <VMDetails {...props} selectedVM={selectedVM} />}
    </div>
  )
}

// "VIRTUAL MACHINE · HV hv-01" — where this VM sits.
function kicker(vm: VirtualMachine, host?: Hypervisor): string {
  const name = host?.name || vm.hypervisorRef || vm.hypervisorName
  return name ? `VIRTUAL MACHINE · HV ${name}` : 'VIRTUAL MACHINE · UNPLACED'
}

function VMHeader({
  selectedVM,
  checkedNames,
  hypervisors,
  actionsMenuRef,
  actionsMenuOpen,
  setActionsMenuOpen,
  runPrimaryAction,
}: VMWorkspaceProps) {
  const actions: Array<{ value: VMPrimaryAction; label: string }> = [
    { value: 'console', label: 'Console' },
    { value: 'power-on', label: 'Power On' },
    { value: 'power-off', label: 'Power Off' },
    { value: 'redeploy', label: 'Redeploy' },
    { value: 'migrate', label: 'Migrate' },
    { value: 'delete', label: 'Delete' },
  ]
  const multiSelectActive = checkedNames.length > 0

  return (
    <section className="bg-transparent border-0 shadow-none grid grid-cols-[minmax(0,1fr)_auto] items-start gap-[0.85rem] max-sm:grid-cols-1">
      <div className="min-w-0">
        <p className="m-0 font-mono text-[10px] uppercase tracking-[0.14em] text-ink-soft">
          {selectedVM ? kicker(selectedVM, hostOf(selectedVM, hypervisors)) : 'VIRTUAL MACHINE'}
        </p>
        {multiSelectActive ? (
          <h2 className="mt-2 font-mono font-medium text-[30px] tracking-[-0.02em] break-anywhere">
            {checkedNames.length} selected
          </h2>
        ) : (
          selectedVM && <SelectedVMSummary vm={selectedVM} />
        )}
      </div>
      <div className="flex min-w-0 flex-col items-end gap-[0.55rem] max-sm:items-start">
        {!multiSelectActive && selectedVM && <span className={phaseClass(selectedVM.phase)}>{selectedVM.phase}</span>}
        <div className="flex justify-end items-center flex-wrap gap-[6px]" ref={actionsMenuRef}>
          {!multiSelectActive && selectedVM && (
            <>
              <button className="py-[7px] px-[11px] text-[12px] font-medium" onClick={() => runPrimaryAction('console')}>
                Console
              </button>
              <button className="py-[7px] px-[11px] text-[12px] font-medium" onClick={() => runPrimaryAction('redeploy')}>
                Redeploy
              </button>
            </>
          )}
          <div className="relative">
            <button
              className="bg-brand border-brand-strong text-white py-[7px] px-[11px] text-[12px] font-medium"
              disabled={!multiSelectActive && !selectedVM}
              onClick={() => setActionsMenuOpen((current) => !current)}
            >
              Actions ▾
            </button>
            {actionsMenuOpen && (
              <div className="absolute right-0 mt-1 min-w-[180px] bg-panel border border-line shadow-[0_10px_24px_rgba(52,43,34,0.16)] z-10">
                {actions.map((item) => (
                  <button
                    key={item.value}
                    className={clsx(
                      'w-full text-left border-0 shadow-none rounded-none px-[0.7rem] py-[0.5rem]',
                      item.value === 'power-off' || item.value === 'delete'
                        ? 'text-danger hover:bg-danger-bg'
                        : 'text-ink hover:bg-panel-2'
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
  )
}

function SelectedVMSummary({ vm }: { vm: VirtualMachine }) {
  const placement = vm.hypervisorRef || (vm.hypervisorName ? `auto → ${vm.hypervisorName}` : 'auto (pending)')
  const address = vm.ipAddresses?.[0] || vm.libvirtDomain || '—'
  return (
    <>
      <h2 className="mt-2 font-mono font-medium text-[30px] tracking-[-0.02em] break-anywhere">{vm.name}</h2>
      <p className="m-0 mt-1 text-[12px] text-ink-soft flex items-center flex-wrap gap-2 break-anywhere">
        <span className="font-mono">
          {placement} · {vm.resources.cpuCores}c · {Math.round(vm.resources.memoryMB / 1024)}g · {address}
        </span>
        <span className="shrink-0 font-mono text-[10.5px] border border-ok-line bg-brand-wash text-brand-accent py-[2px] px-[5px]">
          {vm.ipAssignment === 'static' ? 'STATIC' : 'DHCP'}
        </span>
      </p>
    </>
  )
}

function VMDetails({
  selectedVM,
  hypervisors,
  formatOSImageReference,
  formatCloudInitReferences,
}: VMWorkspaceProps & { selectedVM: VirtualMachine }) {
  return (
    <>
      <VMResourceShareCard vm={selectedVM} hypervisor={hostOf(selectedVM, hypervisors)} />

      <section className="grid grid-cols-1 lg:grid-cols-2 gap-[18px]">
        <FactList
          rows={[
            ['Phase', <span className={phaseClass(selectedVM.phase)}>{selectedVM.phase}</span>],
            ['Power', selectedVM.lastPowerAction || '—'],
            ['Created On', selectedVM.createdOnHost || '—', 'mono'],
            ['Last Error', selectedVM.lastError || '—'],
          ]}
        />
        <FactList
          rows={[
            ['OS Image', formatOSImageReference(selectedVM.osImageRef), 'mono'],
            ['Cloud-Init', formatCloudInitReferences(selectedVM), 'mono'],
            ['DNS Domain', selectedVM.domain || '—', 'mono'],
            ['Updated', formatDate(selectedVM.updatedAt), 'mono'],
          ]}
        />
      </section>

      {selectedVM.advancedOptions && <AdvancedOptions selectedVM={selectedVM} />}

      {selectedVM.networkInterfaces && selectedVM.networkInterfaces.length > 0 && (
        <RuntimeNetworkInterfaces selectedVM={selectedVM} />
      )}
    </>
  )
}

/** Advanced libvirt tuning is only editable in the dialogs, so the detail pane
 *  stays the one place it can be read back after creation. */
function AdvancedOptions({ selectedVM }: { selectedVM: VirtualMachine }) {
  const options = selectedVM.advancedOptions
  if (!options) return null
  const pinning = options.cpuPinning ?? {}
  const rows: FactRow[] = []
  if (options.cpuMode) rows.push(['CPU Mode', options.cpuMode, 'mono'])
  if (options.diskDriver) rows.push(['Disk Driver', options.diskDriver, 'mono'])
  if (options.diskFormat) rows.push(['Disk Format', options.diskFormat, 'mono'])
  if ((options.ioThreads ?? 0) > 0) rows.push(['IO Threads', options.ioThreads, 'mono'])
  if ((options.netMultiqueue ?? 0) > 0) rows.push(['Multiqueue', options.netMultiqueue, 'mono'])
  if (Object.keys(pinning).length > 0) {
    rows.push(['CPU Pinning', Object.entries(pinning).map(([vcpu, cpu]) => `${vcpu}:${cpu}`).join(', '), 'mono'])
  }
  if (rows.length === 0) return null

  return (
    <section className="border border-line bg-panel p-[14px_16px]">
      <h3 className="m-0 mb-[10px] font-mono font-semibold text-[10px] uppercase tracking-[0.14em] text-ink-soft">
        Advanced Options
      </h3>
      <FactList rows={rows} />
    </section>
  )
}

type FactRow = [label: string, value: ReactNode, mono?: 'mono']

function FactList({ rows }: { rows: FactRow[] }) {
  return (
    <dl className="m-0 grid grid-cols-[104px_minmax(0,1fr)] gap-[7px_10px]">
      {rows.map(([label, value, mono]) => (
        <div key={label} className="contents">
          <dt className="font-mono text-[10.5px] uppercase tracking-[0.08em] text-ink-soft self-center">{label}</dt>
          <dd className={clsx('m-0 text-[12px] min-w-0 break-anywhere', mono && 'font-mono')}>{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function RuntimeNetworkInterfaces({ selectedVM }: { selectedVM: VirtualMachine }) {
  return (
    <section className="border border-line bg-panel p-[14px_16px]">
      <h3 className="m-0 mb-[10px] font-mono font-semibold text-[10px] uppercase tracking-[0.14em] text-ink-soft">
        Runtime Network Interfaces
      </h3>
      <div className="overflow-auto">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>MAC</th>
              <th>IP Addresses</th>
            </tr>
          </thead>
          <tbody>
            {selectedVM.networkInterfaces?.map((nic, index) => (
              <tr key={`${nic.name || 'nic'}-${index}`}>
                <td className="font-mono text-[11.5px]">{nic.name || '—'}</td>
                <td className="font-mono text-[11.5px]">{nic.mac || '—'}</td>
                <td className="font-mono text-[11.5px]">
                  {nic.ipAddresses && nic.ipAddresses.length > 0 ? nic.ipAddresses.join(', ') : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
