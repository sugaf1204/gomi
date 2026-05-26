import type { Dispatch, ReactNode, RefObject, SetStateAction } from 'react'
import clsx from 'clsx'
import { formatDate, phaseClass } from '../../../lib/formatters'
import type { VirtualMachine } from '../../../types'
import { VMConsolePanel } from '../VMConsolePanel'
import { initialForm } from './vmFormState'
import type { VMForm, VMPrimaryAction } from './vmFormState'

type VMWorkspaceProps = {
  virtualMachines: VirtualMachine[]
  dataLoading: boolean
  quickDeploying: boolean
  onQuickDeploy: () => void
  onOpenQuickDeploySettings: () => void
  setForm: Dispatch<SetStateAction<VMForm>>
  setAdvancedOpen: Dispatch<SetStateAction<boolean>>
  setFormOpen: Dispatch<SetStateAction<boolean>>
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

export function VMWorkspace({
  virtualMachines,
  dataLoading,
  quickDeploying,
  onQuickDeploy,
  onOpenQuickDeploySettings,
  setForm,
  setAdvancedOpen,
  setFormOpen,
  checkedVMs,
  checkedNames,
  selected,
  selectedVM,
  toggleChecked,
  toggleAllChecked,
  setVMSelection,
  actionsMenuRef,
  actionsMenuOpen,
  setActionsMenuOpen,
  runPrimaryAction,
  consoleVM,
  setConsoleVM,
  formatOSImageReference,
  formatCloudInitReferences
}: VMWorkspaceProps) {
  return (
    <section className="min-h-0 grid grid-cols-1 md:grid-cols-[310px_minmax(0,1fr)] gap-[0.9rem]">
      <div className="min-h-0 grid grid-rows-[auto_minmax(0,1fr)] gap-[0.6rem]">
        <div className="grid gap-[0.45rem]">
          <div className="flex justify-between items-center gap-2">
            <h2 className="text-[1.4rem]">Virtual Machines</h2>
            <div className="flex items-center justify-end flex-wrap gap-[0.35rem]">
              <button className="bg-brand border-brand-strong text-white py-[0.35rem] px-[0.55rem] text-[0.82rem]" disabled={quickDeploying} onClick={onQuickDeploy}>
                {quickDeploying ? 'Deploying...' : 'Quick Deploy'}
              </button>
              <button className="py-[0.35rem] px-[0.55rem] text-[0.82rem]" disabled={quickDeploying} onClick={onOpenQuickDeploySettings}>Preset</button>
              <button
                className="py-[0.35rem] px-[0.55rem] text-[0.82rem]"
                onClick={() => {
                  setForm({ ...initialForm })
                  setAdvancedOpen(false)
                  setFormOpen(true)
                }}
              >
                Create
              </button>
            </div>
          </div>
        </div>

        <VMList
          virtualMachines={virtualMachines}
          dataLoading={dataLoading}
          checkedVMs={checkedVMs}
          selected={selected}
          toggleChecked={toggleChecked}
          toggleAllChecked={toggleAllChecked}
          setVMSelection={setVMSelection}
        />
      </div>

      <div className="min-h-0 grid content-start gap-[0.85rem]">
        {!selectedVM && checkedNames.length === 0 && (
          <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.45rem]">
            <h2>Select a virtual machine</h2>
            <p>Choose a VM from the list to view details and actions.</p>
          </section>
        )}

        {(selectedVM || checkedNames.length > 0) && (
          <VMHeader
            selectedVM={selectedVM}
            checkedNames={checkedNames}
            actionsMenuRef={actionsMenuRef}
            actionsMenuOpen={actionsMenuOpen}
            setActionsMenuOpen={setActionsMenuOpen}
            runPrimaryAction={runPrimaryAction}
          />
        )}

        {consoleVM && (() => {
          const cvmObj = virtualMachines.find((v) => v.name === consoleVM)
          return cvmObj ? <VMConsolePanel vm={cvmObj} onClose={() => setConsoleVM(null)} /> : null
        })()}

        {selectedVM && (
          <VMDetails
            selectedVM={selectedVM}
            formatOSImageReference={formatOSImageReference}
            formatCloudInitReferences={formatCloudInitReferences}
          />
        )}
      </div>
    </section>
  )
}

function VMList({
  virtualMachines,
  dataLoading,
  checkedVMs,
  selected,
  toggleChecked,
  toggleAllChecked,
  setVMSelection
}: Pick<VMWorkspaceProps, 'virtualMachines' | 'dataLoading' | 'checkedVMs' | 'selected' | 'toggleChecked' | 'toggleAllChecked' | 'setVMSelection'>) {
  return (
    <div className="overflow-auto border-t border-line pr-[0.1rem]">
      {dataLoading && virtualMachines.length === 0 && (
        <div className="grid place-items-center py-[2.4rem] text-center text-ink-soft gap-[0.55rem]">
          <div className="loading-spinner" aria-hidden="true" />
          <p className="m-0 text-[0.84rem]">Loading virtual machines...</p>
        </div>
      )}
      {virtualMachines.length > 0 && (
        <div className="flex items-center gap-[0.45rem] py-[0.35rem] pl-[0.55rem] border-b border-line bg-[#f9f7f4]">
          <input type="checkbox" className="w-[0.95rem] h-[0.95rem] m-0 shrink-0 accent-[#2b7a78] cursor-pointer" checked={checkedVMs.size === virtualMachines.length && virtualMachines.length > 0} onChange={toggleAllChecked} />
          <span className="text-[0.78rem] text-ink-soft">Select All</span>
        </div>
      )}
      {virtualMachines.map((vm) => (
        <div key={vm.name} className={clsx('flex items-start border-0 border-b border-line border-l-[3px] border-l-transparent', selected === vm.name && '!border-l-brand bg-[rgba(43,122,120,0.06)]')}>
          <div className="flex items-center pl-[0.55rem] pt-[0.72rem] shrink-0">
            <input type="checkbox" className="w-[0.95rem] h-[0.95rem] m-0 accent-[#2b7a78] cursor-pointer" checked={checkedVMs.has(vm.name)} onChange={() => toggleChecked(vm.name)} />
          </div>
          <button className="text-left flex-1 min-w-0 border-0 bg-transparent flex justify-between items-start gap-[0.75rem] py-[0.62rem] pl-[0.45rem] pr-[0.1rem] shadow-none hover:transform-none!" onClick={() => setVMSelection(vm.name)}>
            <div className="min-w-0">
              <p className="m-0 font-ui font-medium tracking-normal truncate">{vm.name}</p>
              <p className="m-0 text-ink-soft text-[0.82rem]">{vm.hypervisorRef || 'Auto-placed'} - {vm.resources.cpuCores}CPU - {vm.resources.memoryMB}MB</p>
              {vm.phase === 'Error' && vm.lastError && <p className="m-0 text-[#7f2727] text-[0.76rem] mt-[0.15rem] leading-tight">{vm.lastError}</p>}
            </div>
            <span className={clsx(phaseClass(vm.phase), 'shrink-0')}>{vm.phase}</span>
          </button>
        </div>
      ))}
      {!dataLoading && virtualMachines.length === 0 && <p className="m-0 text-ink-soft">No virtual machines found</p>}
    </div>
  )
}

function VMHeader({
  selectedVM,
  checkedNames,
  actionsMenuRef,
  actionsMenuOpen,
  setActionsMenuOpen,
  runPrimaryAction
}: Pick<VMWorkspaceProps, 'selectedVM' | 'checkedNames' | 'actionsMenuRef' | 'actionsMenuOpen' | 'setActionsMenuOpen' | 'runPrimaryAction'>) {
  const actions: Array<{ value: VMPrimaryAction, label: string }> = [
    { value: 'console', label: 'Console' },
    { value: 'power-on', label: 'Power On' },
    { value: 'power-off', label: 'Power Off' },
    { value: 'redeploy', label: 'Redeploy' },
    { value: 'migrate', label: 'Migrate' },
    { value: 'delete', label: 'Delete' }
  ]

  return (
    <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] flex justify-between items-start gap-4">
      <div>
        <p className="m-0 font-ui font-medium text-[0.72rem] uppercase tracking-[0.08em] text-ink-soft">Virtual Machine</p>
        {checkedNames.length > 0 ? (
          <h2 className="mt-[0.22rem] text-[1.8rem]">{checkedNames.length} selected</h2>
        ) : selectedVM && (
          <>
            <h2 className="mt-[0.22rem] text-[1.8rem]">{selectedVM.name}</h2>
            <p className="m-0 text-ink-soft">{selectedVM.hypervisorRef ? selectedVM.hypervisorRef : selectedVM.hypervisorName ? `Auto-placed on: ${selectedVM.hypervisorName}` : 'Auto-placement (pending)'} - PXE</p>
          </>
        )}
      </div>
      <div className="flex flex-col items-end gap-[0.55rem]">
        {checkedNames.length === 0 && selectedVM && <span className={phaseClass(selectedVM.phase)}>{selectedVM.phase}</span>}
        <div className="flex justify-end items-center flex-wrap gap-[0.35rem]" ref={actionsMenuRef}>
          <div className="relative">
            <button className="py-[0.45rem] px-[0.72rem]" disabled={checkedNames.length === 0 && !selectedVM} onClick={() => setActionsMenuOpen((current) => !current)}>Actions</button>
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

function VMDetails({
  selectedVM,
  formatOSImageReference,
  formatCloudInitReferences
}: Pick<VMWorkspaceProps, 'selectedVM' | 'formatOSImageReference' | 'formatCloudInitReferences'> & { selectedVM: VirtualMachine }) {
  return (
    <>
      <section className="grid grid-cols-1 lg:grid-cols-2 gap-[0.85rem]">
        <InfoCard title="Resources" rows={[
          ['CPU Cores', selectedVM.resources.cpuCores],
          ['Memory (MB)', selectedVM.resources.memoryMB],
          ['Disk (GB)', selectedVM.resources.diskGB],
          ['Power Control', selectedVM.powerControlMethod]
        ]} />
        <StatusCard selectedVM={selectedVM} />
      </section>

      <section className="grid grid-cols-1 lg:grid-cols-2 gap-[0.85rem]">
        <InfoCard title="Provisioning" rows={[
          ['Active', selectedVM.provisioning?.active ? 'Yes' : 'No'],
          ['Started', formatDate(selectedVM.provisioning?.startedAt)],
          ['Deadline', formatDate(selectedVM.provisioning?.deadlineAt)],
          ['Completed', formatDate(selectedVM.provisioning?.completedAt)],
          ['Completion Source', selectedVM.provisioning?.completionSource || '-'],
          ['Last Signal', formatDate(selectedVM.provisioning?.lastSignalAt)]
        ]} />
        <InfoCard title="References" rows={[
          ['Hypervisor', selectedVM.hypervisorRef || 'Auto (scheduled by server)'],
          ['Subnet', selectedVM.subnetRef || '-'],
          ['DNS Domain', selectedVM.domain || '-'],
          ['OS Image', formatOSImageReference(selectedVM.osImageRef)],
          ['Cloud-Init', formatCloudInitReferences(selectedVM)],
          ['Install Config', selectedVM.installCfg?.type || '-'],
          ['Last Deployed Cloud-Init', selectedVM.lastDeployedCloudInitRef || '-'],
          ['Created', formatDate(selectedVM.createdAt)],
          ['Updated', formatDate(selectedVM.updatedAt)]
        ]} />
      </section>

      {selectedVM.advancedOptions && <AdvancedOptions selectedVM={selectedVM} />}
      {selectedVM.networkInterfaces && selectedVM.networkInterfaces.length > 0 && <RuntimeNetworkInterfaces selectedVM={selectedVM} />}
    </>
  )
}

function InfoCard({ title, rows }: { title: string; rows: Array<[string, ReactNode]> }) {
  return (
    <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
      <h3 className="mb-[0.58rem] text-[1.05rem]">{title}</h3>
      <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
        {rows.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-ink-soft text-[0.84rem]">{label}</dt>
            <dd className="m-0">{value}</dd>
          </div>
        ))}
      </dl>
    </article>
  )
}

function StatusCard({ selectedVM }: { selectedVM: VirtualMachine }) {
  return (
    <InfoCard title="Status" rows={[
      ['Phase', <span className={phaseClass(selectedVM.phase)}>{selectedVM.phase}</span>],
      ['Hypervisor', selectedVM.hypervisorName || '-'],
      ['Domain', <code className="text-[0.82rem]">{selectedVM.libvirtDomain || '-'}</code>],
      ['IP Addresses', <><span>{selectedVM.ipAddresses?.join(', ') || '-'}</span> <span className={clsx('text-[0.68rem] font-medium px-[0.35rem] py-[0.05rem] rounded-sm border', selectedVM.ipAssignment === 'static' ? 'bg-[#e8f4f3] text-[#1a6360] border-[#b8dbd9]' : 'bg-[#f3f0ea] text-ink-soft border-[#e0dbd2]')}>{selectedVM.ipAssignment === 'static' ? 'Static' : 'DHCP'}</span></>],
      ['MAC Addresses', selectedVM.networkInterfaces?.map((nic) => nic.mac).filter((mac): mac is string => Boolean(mac)).join(', ') || '-'],
      ['Created On Host', selectedVM.createdOnHost || '-'],
      ['Last Power Action', selectedVM.lastPowerAction || '-'],
      ['Last Error', selectedVM.lastError || '-']
    ]} />
  )
}

function AdvancedOptions({ selectedVM }: { selectedVM: VirtualMachine }) {
  const options = selectedVM.advancedOptions
  if (!options) return null
  return (
    <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
      <h3 className="mb-[0.58rem] text-[1.05rem]">Advanced Options</h3>
      <dl className="m-0 grid grid-cols-[150px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
        {options.cpuMode && <><dt className="text-ink-soft text-[0.84rem]">CPU Mode</dt><dd className="m-0">{options.cpuMode}</dd></>}
        {options.diskDriver && <><dt className="text-ink-soft text-[0.84rem]">Disk Driver</dt><dd className="m-0">{options.diskDriver}</dd></>}
        {options.diskFormat && <><dt className="text-ink-soft text-[0.84rem]">Disk Format</dt><dd className="m-0">{options.diskFormat}</dd></>}
        {(options.ioThreads ?? 0) > 0 && <><dt className="text-ink-soft text-[0.84rem]">IO Threads</dt><dd className="m-0">{options.ioThreads}</dd></>}
        {(options.netMultiqueue ?? 0) > 0 && <><dt className="text-ink-soft text-[0.84rem]">Net Multiqueue</dt><dd className="m-0">{options.netMultiqueue}</dd></>}
        {options.cpuPinning && Object.keys(options.cpuPinning).length > 0 && <><dt className="text-ink-soft text-[0.84rem]">CPU Pinning</dt><dd className="m-0"><code className="text-[0.82rem]">{Object.entries(options.cpuPinning).map(([v, c]) => `${v}:${c}`).join(', ')}</code></dd></>}
      </dl>
    </section>
  )
}

function RuntimeNetworkInterfaces({ selectedVM }: { selectedVM: VirtualMachine }) {
  return (
    <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
      <h3 className="mb-[0.58rem] text-[1.05rem]">Runtime Network Interfaces</h3>
      <div className="overflow-auto">
        <table>
          <thead><tr><th>Name</th><th>MAC</th><th>IP Addresses</th></tr></thead>
          <tbody>
            {selectedVM.networkInterfaces?.map((nic, i) => (
              <tr key={`${nic.name || 'nic'}-${i}`}>
                <td>{nic.name || '-'}</td>
                <td><code className="text-[0.82rem]">{nic.mac || '-'}</code></td>
                <td>{nic.ipAddresses && nic.ipAddresses.length > 0 ? nic.ipAddresses.join(', ') : '-'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
