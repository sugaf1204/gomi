import type { Dispatch, FormEvent, ReactNode, SetStateAction } from 'react'
import clsx from 'clsx'
import { ModalOverlay } from '../../ui/ModalOverlay'
import type { Machine } from '../../../types'
import {
  initialBatchDeleteConfirmState,
  initialBatchPowerConfirmState,
  initialBatchRedeployConfirmState
} from './machineFormState'
import type {
  BatchDeleteConfirmState,
  BatchPowerConfirmState,
  BatchRedeployConfirmState,
  MachineDialogState,
  MachineFormState,
  UpdateMachineForm
} from './machineFormState'

type MachineDialogsProps = {
  machineDialog: MachineDialogState
  closeMachineDialog: () => void
  selectedMachine: Machine | null
  form: MachineFormState
  setForm: Dispatch<SetStateAction<MachineFormState>>
  submitMachineDialog: (event?: FormEvent) => void
  renderMachineSpecFields: (formState: MachineFormState, updateForm: UpdateMachineForm, radioName: string) => ReactNode
  batchRedeployConfirm: BatchRedeployConfirmState
  setBatchRedeployConfirm: Dispatch<SetStateAction<BatchRedeployConfirmState>>
  updateBatchRedeployForm: (target: string, updater: (current: MachineFormState) => MachineFormState) => void
  machineFormReady: (formState: MachineFormState) => boolean
  submitBatchRedeployConfirm: () => void
  batchPowerConfirm: BatchPowerConfirmState
  setBatchPowerConfirm: Dispatch<SetStateAction<BatchPowerConfirmState>>
  submitBatchPowerConfirm: () => void
  batchDeleteConfirm: BatchDeleteConfirmState
  setBatchDeleteConfirm: Dispatch<SetStateAction<BatchDeleteConfirmState>>
  submitBatchDeleteConfirm: () => void
}

export function MachineDialogs(props: MachineDialogsProps) {
  return (
    <>
      <MachineEditDialog {...props} />
      <BatchRedeployDialog {...props} />
      <BatchPowerDialog {...props} />
      <BatchDeleteDialog {...props} />
    </>
  )
}

function MachineEditDialog({ machineDialog, closeMachineDialog, selectedMachine, form, setForm, submitMachineDialog, renderMachineSpecFields }: MachineDialogsProps) {
  if (!machineDialog.open) return null
  return (
    <ModalOverlay onBackdropClick={() => { if (!machineDialog.running) closeMachineDialog() }}>
      <div className="w-[min(620px,100%)] bg-panel border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
        <DialogHeader title={machineDialog.mode === 'create' ? 'Add Machine' : `Redeploy: ${machineDialog.machineName}`} disabled={machineDialog.running} onClose={closeMachineDialog} />
        {machineDialog.mode === 'redeploy' && selectedMachine && (
          <div className="grid gap-[0.3rem] bg-panel-2 border border-line p-[0.6rem] text-[0.84rem]">
            <p className="m-0 text-ink-soft">OS Preset: <strong className="text-ink">{selectedMachine.osPreset.family} {selectedMachine.osPreset.version}</strong></p>
            <p className="m-0 text-ink-soft">Current Phase: <strong className="text-ink">{selectedMachine.phase}</strong></p>
            {(selectedMachine.phase.toLowerCase() === 'running' || selectedMachine.phase.toLowerCase() === 'ready') && <p className="m-0 text-warn font-medium mt-[0.15rem]">This machine is currently active. Redeploying will disrupt services.</p>}
          </div>
        )}
        <form className="grid gap-[0.55rem]" onSubmit={(e) => submitMachineDialog(e)}>
          {renderMachineSpecFields(form, (updater) => setForm((current) => updater(current)), `machine-${machineDialog.mode}`)}
          <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
            <button type="button" onClick={closeMachineDialog} disabled={machineDialog.running}>Cancel</button>
            <button type="submit" disabled={machineDialog.running || !machineFormReadyForSubmit(form)} className={clsx('text-white', machineDialog.mode === 'create' ? 'bg-brand border-brand-strong' : 'bg-danger-line border-danger')}>
              {machineDialog.running ? (machineDialog.mode === 'create' ? 'Creating...' : 'Redeploying...') : (machineDialog.mode === 'create' ? 'Create Machine' : 'Redeploy')}
            </button>
          </div>
        </form>
      </div>
    </ModalOverlay>
  )
}

function BatchRedeployDialog({ batchRedeployConfirm, setBatchRedeployConfirm, updateBatchRedeployForm, machineFormReady, submitBatchRedeployConfirm, renderMachineSpecFields }: MachineDialogsProps) {
  if (!batchRedeployConfirm.open) return null
  const activeTarget = batchRedeployConfirm.activeTarget || batchRedeployConfirm.targets[0]
  const activeForm = batchRedeployConfirm.forms[activeTarget]
  const invalidTargets = batchRedeployConfirm.targets.filter((target) => !machineFormReady(batchRedeployConfirm.forms[target]))
  const close = () => setBatchRedeployConfirm(initialBatchRedeployConfirmState)
  return (
    <ModalOverlay onBackdropClick={() => { if (!batchRedeployConfirm.running) close() }}>
      <div className="w-[min(980px,100%)] bg-panel border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.8rem] max-h-[90vh] overflow-auto">
        <DialogHeader title="Bulk Redeploy Machines" disabled={batchRedeployConfirm.running} onClose={close}>
          <p className="m-0 text-ink-soft text-[0.84rem]">Edit each machine before running redeploy sequentially.</p>
        </DialogHeader>
        <div className="grid grid-cols-[240px_minmax(0,1fr)] gap-[0.85rem] max-md:grid-cols-1">
          <RedeployTargetList confirm={batchRedeployConfirm} activeTarget={activeTarget} setConfirm={setBatchRedeployConfirm} />
          <div className="min-w-0 grid gap-[0.55rem]">
            {activeForm && (
              <>
                <div className="bg-panel-2 border border-line p-[0.6rem] text-[0.84rem]">
                  <p className="m-0 text-ink-soft">Editing <strong className="text-ink">{activeTarget}</strong></p>
                  {invalidTargets.includes(activeTarget) && <p className="m-0 mt-[0.2rem] text-danger font-medium">Required fields are missing for this target.</p>}
                </div>
                <fieldset disabled={batchRedeployConfirm.running} className="grid gap-[0.55rem] border-0 p-0 m-0 min-w-0 disabled:opacity-80">
                  {renderMachineSpecFields(activeForm, (updater) => updateBatchRedeployForm(activeTarget, updater), `machine-bulk-${activeTarget}`)}
                </fieldset>
              </>
            )}
          </div>
        </div>
        <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
          <button type="button" onClick={close} disabled={batchRedeployConfirm.running}>Cancel</button>
          <button type="button" disabled={batchRedeployConfirm.running || invalidTargets.length > 0} className="bg-danger-line border-danger text-white" onClick={submitBatchRedeployConfirm}>
            {batchRedeployConfirm.running ? 'Redeploying...' : `Redeploy all (${batchRedeployConfirm.targets.length})`}
          </button>
        </div>
      </div>
    </ModalOverlay>
  )
}

function BatchPowerDialog({ batchPowerConfirm, setBatchPowerConfirm, submitBatchPowerConfirm }: MachineDialogsProps) {
  if (!batchPowerConfirm.open) return null
  const close = () => setBatchPowerConfirm(initialBatchPowerConfirmState)
  return (
    <ConfirmTargetsDialog
      title={batchPowerConfirm.action === 'power-on' ? 'Confirm Power On' : 'Confirm Power Off'}
      targets={batchPowerConfirm.targets}
      running={batchPowerConfirm.running}
      onClose={close}
      onConfirm={submitBatchPowerConfirm}
      confirmClass={batchPowerConfirm.action === 'power-on' ? 'bg-brand border-brand-strong text-white' : 'bg-danger-line border-danger text-white'}
      confirmLabel={batchPowerConfirm.running ? (batchPowerConfirm.action === 'power-on' ? 'Powering On...' : 'Powering Off...') : (batchPowerConfirm.action === 'power-on' ? 'Power On' : 'Power Off')}
    />
  )
}

function BatchDeleteDialog({ batchDeleteConfirm, setBatchDeleteConfirm, submitBatchDeleteConfirm }: MachineDialogsProps) {
  if (!batchDeleteConfirm.open) return null
  return (
    <ConfirmTargetsDialog
      title="Confirm Batch Delete"
      titleClass="text-danger"
      description="Linked hypervisor records and their virtual machine records are also removed from GOMI. No changes are made on the hosts."
      targets={batchDeleteConfirm.targets}
      running={batchDeleteConfirm.running}
      onClose={() => setBatchDeleteConfirm(initialBatchDeleteConfirmState)}
      onConfirm={submitBatchDeleteConfirm}
      confirmClass="bg-danger-line border-danger text-white"
      confirmLabel={batchDeleteConfirm.running ? 'Deleting...' : 'Delete'}
    />
  )
}

function DialogHeader({ title, disabled, onClose, children }: { title: string; disabled: boolean; onClose: () => void; children?: ReactNode }) {
  return (
    <div className="flex justify-between items-center gap-[0.8rem]">
      <div>
        <h3 className="text-[1.2rem]">{title}</h3>
        {children}
      </div>
      <button aria-label="Close" className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!" disabled={disabled} onClick={onClose}>x</button>
    </div>
  )
}

function RedeployTargetList({ confirm, activeTarget, setConfirm }: { confirm: BatchRedeployConfirmState; activeTarget: string; setConfirm: Dispatch<SetStateAction<BatchRedeployConfirmState>> }) {
  return (
    <div className="border border-line bg-panel-2 p-[0.55rem] max-h-[68vh] overflow-auto">
      <p className="m-0 mb-[0.45rem] text-ink-soft text-[0.78rem]">Targets ({confirm.targets.length})</p>
      <div className="grid gap-[0.35rem]">
        {confirm.targets.map((target) => {
          const status = confirm.status[target]?.state ?? 'pending'
          return (
            <button key={target} type="button" className={clsx('w-full text-left border border-line shadow-none px-[0.55rem] py-[0.48rem] bg-panel hover:bg-panel-3', activeTarget === target && 'border-brand bg-brand-wash')} onClick={() => setConfirm((current) => ({ ...current, activeTarget: target }))}>
              <span className="block font-medium break-anywhere">{target}</span>
              <span className={clsx('mt-[0.18rem] inline-flex text-[0.68rem] font-medium px-[0.35rem] py-[0.05rem] rounded-sm border', status === 'succeeded' && 'bg-ok-bg text-ok border-ok-line', status === 'failed' && 'bg-danger-bg text-danger border-error-line', status === 'running' && 'bg-warn-bg text-warn border-warn-line', status === 'pending' && 'bg-panel-3 text-ink-soft border-line-soft')}>{status}</span>
              {confirm.status[target]?.error && <span className="block mt-[0.2rem] text-danger text-[0.72rem] leading-tight">{confirm.status[target]?.error}</span>}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function ConfirmTargetsDialog({ title, titleClass, description, targets, running, onClose, onConfirm, confirmClass, confirmLabel }: { title: string; titleClass?: string; description?: string; targets: string[]; running: boolean; onClose: () => void; onConfirm: () => void; confirmClass: string; confirmLabel: string }) {
  return (
    <ModalOverlay onBackdropClick={() => { if (!running) onClose() }}>
      <div className="w-[min(520px,100%)] bg-panel border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
        <h3 className={clsx('text-[1.2rem]', titleClass)}>{title}</h3>
        {description && <p className="m-0 text-ink-soft text-[0.84rem]">{description}</p>}
        <p className="m-0 text-ink-soft text-[0.84rem]">Target machines ({targets.length}):</p>
        <div className="max-h-[180px] overflow-auto border border-line p-[0.55rem] bg-panel-2"><ul className="m-0 pl-[1.1rem]">{targets.map((target) => <li key={target}><code>{target}</code></li>)}</ul></div>
        <div className="flex justify-end gap-[0.45rem]">
          <button type="button" onClick={onClose} disabled={running}>Cancel</button>
          <button type="button" onClick={onConfirm} disabled={running} className={confirmClass}>{confirmLabel}</button>
        </div>
      </div>
    </ModalOverlay>
  )
}

function machineFormReadyForSubmit(form: MachineFormState) {
  return Boolean(form.hostname.trim() && form.mac.trim() && form.imageRef && (!form.subnetRef || form.ipAssignment !== 'static' || form.staticIP.trim()) && (form.cloudInitMode !== 'create' || (form.cloudInitTemplateName.trim() && form.cloudInitUserData.trim())))
}
