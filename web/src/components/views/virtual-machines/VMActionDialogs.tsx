import type { Dispatch, ReactNode, SetStateAction } from 'react'
import clsx from 'clsx'
import { ModalOverlay } from '../../ui/ModalOverlay'
import type { Hypervisor, VirtualMachine } from '../../../types'
import type {
  UpdateVMConfigForm,
  VMBulkRedeployConfirmState,
  VMConfigForm,
  VMDeleteConfirmState,
  VMMigrateConfirmState,
  VMPowerConfirmState,
  VMReinstallForm
} from './vmFormState'
import { initialBulkRedeployConfirm, initialDeleteConfirm, initialMigrateConfirm, initialPowerConfirm } from './vmFormState'

type VMActionDialogsProps = {
  deleteConfirm: VMDeleteConfirmState
  setDeleteConfirm: Dispatch<SetStateAction<VMDeleteConfirmState>>
  onDeleteConfirm: () => void
  bulkRedeployConfirm: VMBulkRedeployConfirmState
  setBulkRedeployConfirm: Dispatch<SetStateAction<VMBulkRedeployConfirmState>>
  setBulkRedeployAdvancedOpen: (target: string, value: boolean | ((current: boolean) => boolean)) => void
  updateBulkRedeployForm: (target: string, updater: (current: VMConfigForm) => VMConfigForm) => void
  vmRedeployFormReady: (formState: VMConfigForm) => boolean
  onBulkRedeployConfirm: () => void
  powerConfirm: VMPowerConfirmState
  setPowerConfirm: Dispatch<SetStateAction<VMPowerConfirmState>>
  onPowerConfirm: () => void
  migrateConfirm: VMMigrateConfirmState
  setMigrateConfirm: Dispatch<SetStateAction<VMMigrateConfirmState>>
  onMigrateConfirm: () => void
  virtualMachines: VirtualMachine[]
  hypervisors: Hypervisor[]
  renderFields: (
    form: VMReinstallForm,
    updateForm: UpdateVMConfigForm,
    advancedOpen: boolean,
    setAdvancedOpen: (value: boolean | ((current: boolean) => boolean)) => void,
    radioName: string
  ) => ReactNode
}

export function VMActionDialogs(props: VMActionDialogsProps) {
  return (
    <>
      <DeleteDialog {...props} />
      <BulkRedeployDialog {...props} />
      <PowerDialog {...props} />
      <MigrateDialog {...props} />
    </>
  )
}

function DeleteDialog({ deleteConfirm, setDeleteConfirm, onDeleteConfirm }: VMActionDialogsProps) {
  if (!deleteConfirm.open) return null
  const close = () => setDeleteConfirm(initialDeleteConfirm)
  return (
    <ModalOverlay onBackdropClick={() => { if (!deleteConfirm.running) close() }}>
      <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
        <div className="flex justify-between items-center">
          <h3 className="text-[1.2rem] text-[#9b2d2d]">Delete Virtual Machine{deleteConfirm.targets.length > 1 ? 's' : ''}</h3>
          <button aria-label="Close" className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!" disabled={deleteConfirm.running} onClick={close}>x</button>
        </div>
        <p className="m-0 text-ink-soft text-[0.84rem]">This action permanently removes runtime resources and cannot be undone.</p>
        <TargetList targets={deleteConfirm.targets} />
        <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
          <button type="button" onClick={close} disabled={deleteConfirm.running}>Cancel</button>
          <button type="button" disabled={deleteConfirm.running} className="bg-[#d86b6b] border-[#be5252] text-white" onClick={onDeleteConfirm}>
            {deleteConfirm.running ? 'Deleting...' : `Delete (${deleteConfirm.targets.length})`}
          </button>
        </div>
      </div>
    </ModalOverlay>
  )
}

function BulkRedeployDialog({
  bulkRedeployConfirm,
  setBulkRedeployConfirm,
  setBulkRedeployAdvancedOpen,
  updateBulkRedeployForm,
  vmRedeployFormReady,
  onBulkRedeployConfirm,
  renderFields
}: VMActionDialogsProps) {
  if (!bulkRedeployConfirm.open) return null
  const activeTarget = bulkRedeployConfirm.activeTarget || bulkRedeployConfirm.targets[0]
  const activeForm = bulkRedeployConfirm.forms[activeTarget]
  const invalidTargets = bulkRedeployConfirm.targets.filter((target) => !vmRedeployFormReady(bulkRedeployConfirm.forms[target]))
  const close = () => setBulkRedeployConfirm(initialBulkRedeployConfirm)

  return (
    <ModalOverlay onBackdropClick={() => { if (!bulkRedeployConfirm.running) close() }}>
      <div className="w-[min(1040px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.8rem] max-h-[90vh] overflow-auto">
        <div className="flex justify-between items-center gap-[0.8rem]">
          <div>
            <h3 className="text-[1.2rem]">Bulk Redeploy Virtual Machines</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">Edit each VM before running redeploy sequentially.</p>
          </div>
          <button aria-label="Close" className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!" disabled={bulkRedeployConfirm.running} onClick={close}>x</button>
        </div>
        <div className="grid grid-cols-[250px_minmax(0,1fr)] gap-[0.85rem] max-md:grid-cols-1">
          <div className="border border-line bg-[#f9f7f4] p-[0.55rem] max-h-[68vh] overflow-auto">
            <p className="m-0 mb-[0.45rem] text-ink-soft text-[0.78rem]">Targets ({bulkRedeployConfirm.targets.length})</p>
            <div className="grid gap-[0.35rem]">
              {bulkRedeployConfirm.targets.map((target) => (
                <RedeployTargetButton
                  key={target}
                  target={target}
                  active={activeTarget === target}
                  status={bulkRedeployConfirm.status[target]}
                  onSelect={() => setBulkRedeployConfirm((current) => ({ ...current, activeTarget: target }))}
                />
              ))}
            </div>
          </div>
          <div className="min-w-0 grid gap-[0.55rem]">
            {activeForm && (
              <>
                <div className="bg-[#f9f7f4] border border-line p-[0.6rem] text-[0.84rem]">
                  <p className="m-0 text-ink-soft">Editing <strong className="text-ink">{activeTarget}</strong></p>
                  {invalidTargets.includes(activeTarget) && <p className="m-0 mt-[0.2rem] text-[#9b2d2d] font-medium">Required fields are missing for this target.</p>}
                </div>
                <fieldset disabled={bulkRedeployConfirm.running} className="grid gap-[0.55rem] border-0 p-0 m-0 min-w-0 disabled:opacity-80">
                  {renderFields(
                    activeForm,
                    (updater) => updateBulkRedeployForm(activeTarget, updater),
                    bulkRedeployConfirm.advancedOpen[activeTarget] ?? false,
                    (value) => setBulkRedeployAdvancedOpen(activeTarget, value),
                    `vm-bulk-${activeTarget}`
                  )}
                </fieldset>
              </>
            )}
          </div>
        </div>
        <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
          <button type="button" onClick={close} disabled={bulkRedeployConfirm.running}>Cancel</button>
          <button type="button" disabled={bulkRedeployConfirm.running || invalidTargets.length > 0} className="bg-brand border-brand-strong text-white" onClick={onBulkRedeployConfirm}>
            {bulkRedeployConfirm.running ? 'Redeploying...' : `Redeploy all (${bulkRedeployConfirm.targets.length})`}
          </button>
        </div>
      </div>
    </ModalOverlay>
  )
}

function PowerDialog({ powerConfirm, setPowerConfirm, onPowerConfirm }: VMActionDialogsProps) {
  if (!powerConfirm.open) return null
  const close = () => setPowerConfirm(initialPowerConfirm)
  return (
    <ModalOverlay onBackdropClick={() => { if (!powerConfirm.running) close() }}>
      <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
        <h3 className="text-[1.2rem]">{powerConfirm.action === 'power-on' ? 'Confirm Power On' : 'Confirm Power Off'}</h3>
        <p className="m-0 text-ink-soft text-[0.84rem]">Target virtual machines ({powerConfirm.targets.length}):</p>
        <TargetList targets={powerConfirm.targets} />
        <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
          <button type="button" onClick={close} disabled={powerConfirm.running}>Cancel</button>
          <button type="button" disabled={powerConfirm.running} className={clsx('text-white', powerConfirm.action === 'power-on' ? 'bg-brand border-brand-strong' : 'bg-[#d86b6b] border-[#be5252]')} onClick={onPowerConfirm}>
            {powerConfirm.running ? (powerConfirm.action === 'power-on' ? 'Powering On...' : 'Powering Off...') : (powerConfirm.action === 'power-on' ? 'Power On' : 'Power Off')}
          </button>
        </div>
      </div>
    </ModalOverlay>
  )
}

function MigrateDialog({ migrateConfirm, setMigrateConfirm, onMigrateConfirm, virtualMachines, hypervisors }: VMActionDialogsProps) {
  if (!migrateConfirm.open) return null
  const migrateVM = virtualMachines.find((v) => v.name === migrateConfirm.vmName)
  const currentHV = migrateVM?.hypervisorName || migrateVM?.hypervisorRef || ''
  const otherHVs = hypervisors.filter((hv) => hv.name !== currentHV)
  const close = () => setMigrateConfirm(initialMigrateConfirm)

  return (
    <ModalOverlay onBackdropClick={() => { if (!migrateConfirm.running) close() }}>
      <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
        <div className="flex justify-between items-center">
          <h3 className="text-[1.2rem]">Migrate VM: {migrateConfirm.vmName}</h3>
          <button aria-label="Close" className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!" disabled={migrateConfirm.running} onClick={close}>x</button>
        </div>
        <p className="m-0 text-ink-soft text-[0.84rem]">Live-migrate the running VM to another hypervisor. The VM will remain online during migration.</p>
        <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
          <dt className="text-ink-soft text-[0.84rem]">Source Hypervisor</dt>
          <dd className="m-0 font-medium">{currentHV || 'Unknown'}</dd>
        </dl>
        <label className="text-[0.84rem]">
          Target Hypervisor
          <select value={migrateConfirm.targetHypervisor} onChange={(e) => setMigrateConfirm((prev) => ({ ...prev, targetHypervisor: e.target.value }))} disabled={migrateConfirm.running}>
            <option value="">Auto (best available)</option>
            {otherHVs.map((hv) => <option key={hv.name} value={hv.name}>{hv.name} ({hv.connection.host})</option>)}
          </select>
        </label>
        <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
          <button type="button" onClick={close} disabled={migrateConfirm.running}>Cancel</button>
          <button type="button" disabled={migrateConfirm.running} className="bg-brand border-brand-strong text-white" onClick={onMigrateConfirm}>
            {migrateConfirm.running ? 'Migrating...' : 'Migrate'}
          </button>
        </div>
      </div>
    </ModalOverlay>
  )
}

function TargetList({ targets }: { targets: string[] }) {
  return (
    <div className="max-h-[180px] overflow-auto border border-line p-[0.55rem] bg-[#f9f7f4]">
      <ul className="m-0 pl-[1.1rem]">
        {targets.map((target) => <li key={target}><code>{target}</code></li>)}
      </ul>
    </div>
  )
}

function RedeployTargetButton({
  target,
  active,
  status,
  onSelect
}: {
  target: string
  active: boolean
  status?: { state: string; error?: string }
  onSelect: () => void
}) {
  const state = status?.state ?? 'pending'
  return (
    <button type="button" className={clsx('w-full text-left border border-line shadow-none px-[0.55rem] py-[0.48rem] bg-white hover:bg-[#f3efe8]', active && 'border-brand bg-[rgba(43,122,120,0.08)]')} onClick={onSelect}>
      <span className="block font-medium break-anywhere">{target}</span>
      <span className={clsx(
        'mt-[0.18rem] inline-flex text-[0.68rem] font-medium px-[0.35rem] py-[0.05rem] rounded-sm border',
        state === 'succeeded' && 'bg-[#e9f6ec] text-[#25633a] border-[#b8dec3]',
        state === 'failed' && 'bg-[#fff0ee] text-[#9b2d2d] border-[#ecc1ba]',
        state === 'running' && 'bg-[#eef5ff] text-[#265f9b] border-[#bfd5ee]',
        state === 'pending' && 'bg-[#f3f0ea] text-ink-soft border-[#e0dbd2]'
      )}>{state}</span>
      {status?.error && <span className="block mt-[0.2rem] text-[#9b2d2d] text-[0.72rem] leading-tight">{status.error}</span>}
    </button>
  )
}
