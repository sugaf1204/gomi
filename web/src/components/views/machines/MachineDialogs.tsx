import type { Dispatch, FormEvent, ReactNode, SetStateAction } from 'react'
import clsx from 'clsx'
import { supportsDeploymentTarget } from '../../../lib/osImages'
import { ModalOverlay } from '../../ui/ModalOverlay'
import type { CloudInitTemplate, Machine, OSImage, PowerType, SSHKey, Subnet } from '../../../types'
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
  MachineQuickDeployPreset,
  UpdateMachineForm
} from './machineFormState'

type MachineDialogsProps = {
  quickDeploySettingsOpen: boolean
  quickDeployPreset: MachineQuickDeployPreset
  setQuickDeployPreset: Dispatch<SetStateAction<MachineQuickDeployPreset>>
  quickDeploying: boolean
  osImages: OSImage[]
  cloudInits: CloudInitTemplate[]
  sshKeys: SSHKey[]
  subnets: Subnet[]
  quickDeployMachineName: (preset: MachineQuickDeployPreset) => string
  quickDeployMachineReady: (preset: MachineQuickDeployPreset) => boolean
  onCloseQuickDeploy: () => void
  onQuickDeploy: () => void
  onToggleQuickDeploySSHKeyRef: (ref: string) => void
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
      <QuickDeployDialog {...props} />
      <MachineEditDialog {...props} />
      <BatchRedeployDialog {...props} />
      <BatchPowerDialog {...props} />
      <BatchDeleteDialog {...props} />
    </>
  )
}

function QuickDeployDialog({
  quickDeploySettingsOpen,
  quickDeployPreset,
  setQuickDeployPreset,
  quickDeploying,
  osImages,
  cloudInits,
  sshKeys,
  subnets,
  quickDeployMachineName,
  quickDeployMachineReady,
  onCloseQuickDeploy,
  onQuickDeploy,
  onToggleQuickDeploySSHKeyRef
}: MachineDialogsProps) {
  if (!quickDeploySettingsOpen) return null
  return (
    <ModalOverlay onBackdropClick={() => { if (!quickDeploying) onCloseQuickDeploy() }}>
      <div className="w-[min(680px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
        <DialogHeader title="Quick Deploy Preset" disabled={quickDeploying} onClose={onCloseQuickDeploy}>
          <p className="m-0 text-ink-soft text-[0.82rem]">Next machine: <code>{quickDeployPreset.name.trim() ? quickDeployMachineName(quickDeployPreset) : '-'}</code></p>
        </DialogHeader>
        <div className="grid gap-[0.55rem]">
          <NameCountFields preset={quickDeployPreset} setPreset={setQuickDeployPreset} />
          <label className="text-[0.84rem]">MAC Address<input required value={quickDeployPreset.mac} onChange={(e) => setQuickDeployPreset((current) => ({ ...current, mac: e.target.value }))} placeholder="aa:bb:cc:dd:ee:ff" /></label>
          <PowerPresetFields preset={quickDeployPreset} setPreset={setQuickDeployPreset} />
          <ImagePresetFields preset={quickDeployPreset} setPreset={setQuickDeployPreset} osImages={osImages} />
          <label className="text-[0.84rem]">Target Disk <span className="text-ink-soft">(optional)</span><input value={quickDeployPreset.targetDisk} onChange={(e) => setQuickDeployPreset((current) => ({ ...current, targetDisk: e.target.value }))} placeholder="/dev/disk/by-id/..." /></label>
          <label className="text-[0.84rem]">
            Cloud-Init <span className="text-ink-soft">(optional)</span>
            <select value={quickDeployPreset.cloudInitExistingRef} onChange={(e) => setQuickDeployPreset((current) => ({ ...current, cloudInitExistingRef: e.target.value }))}>
              <option value="">None</option>
              {cloudInits.map((ci) => <option key={ci.name} value={ci.name}>{ci.name}</option>)}
            </select>
          </label>
          <NetworkPresetFields preset={quickDeployPreset} setPreset={setQuickDeployPreset} subnets={subnets} />
          <SSHPresetFields preset={quickDeployPreset} setPreset={setQuickDeployPreset} sshKeys={sshKeys} onToggleSSHKeyRef={onToggleQuickDeploySSHKeyRef} />
          <div className="flex justify-between gap-[0.45rem] pt-[0.2rem]">
            <button type="button" onClick={() => setQuickDeployPreset((current) => ({ ...current, count: '1' }))} disabled={quickDeploying}>Reset Count</button>
            <div className="flex justify-end gap-[0.45rem]">
              <button type="button" onClick={onCloseQuickDeploy} disabled={quickDeploying}>Close</button>
              <button
                type="button"
                className="bg-brand border-brand-strong text-white"
                disabled={quickDeploying || !quickDeployMachineReady(quickDeployPreset)}
                onClick={() => {
                  onCloseQuickDeploy()
                  onQuickDeploy()
                }}
              >
                {quickDeploying ? 'Deploying...' : 'Deploy Now'}
              </button>
            </div>
          </div>
        </div>
      </div>
    </ModalOverlay>
  )
}

function MachineEditDialog({ machineDialog, closeMachineDialog, selectedMachine, form, setForm, submitMachineDialog, renderMachineSpecFields }: MachineDialogsProps) {
  if (!machineDialog.open) return null
  return (
    <ModalOverlay onBackdropClick={() => { if (!machineDialog.running) closeMachineDialog() }}>
      <div className="w-[min(620px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
        <DialogHeader title={machineDialog.mode === 'create' ? 'Add Machine' : `Redeploy: ${machineDialog.machineName}`} disabled={machineDialog.running} onClose={closeMachineDialog} />
        {machineDialog.mode === 'redeploy' && selectedMachine && (
          <div className="grid gap-[0.3rem] bg-[#f9f7f4] border border-line p-[0.6rem] text-[0.84rem]">
            <p className="m-0 text-ink-soft">OS Preset: <strong className="text-ink">{selectedMachine.osPreset.family} {selectedMachine.osPreset.version}</strong></p>
            <p className="m-0 text-ink-soft">Current Phase: <strong className="text-ink">{selectedMachine.phase}</strong></p>
            {(selectedMachine.phase.toLowerCase() === 'running' || selectedMachine.phase.toLowerCase() === 'ready') && <p className="m-0 text-[#8a4f23] font-medium mt-[0.15rem]">This machine is currently active. Redeploying will disrupt services.</p>}
          </div>
        )}
        <form className="grid gap-[0.55rem]" onSubmit={(e) => submitMachineDialog(e)}>
          {renderMachineSpecFields(form, (updater) => setForm((current) => updater(current)), `machine-${machineDialog.mode}`)}
          <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
            <button type="button" onClick={closeMachineDialog} disabled={machineDialog.running}>Cancel</button>
            <button type="submit" disabled={machineDialog.running || !machineFormReadyForSubmit(form)} className={clsx('text-white', machineDialog.mode === 'create' ? 'bg-brand border-brand-strong' : 'bg-[#d86b6b] border-[#be5252]')}>
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
      <div className="w-[min(980px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.8rem] max-h-[90vh] overflow-auto">
        <DialogHeader title="Bulk Redeploy Machines" disabled={batchRedeployConfirm.running} onClose={close}>
          <p className="m-0 text-ink-soft text-[0.84rem]">Edit each machine before running redeploy sequentially.</p>
        </DialogHeader>
        <div className="grid grid-cols-[240px_minmax(0,1fr)] gap-[0.85rem] max-md:grid-cols-1">
          <RedeployTargetList confirm={batchRedeployConfirm} activeTarget={activeTarget} setConfirm={setBatchRedeployConfirm} />
          <div className="min-w-0 grid gap-[0.55rem]">
            {activeForm && (
              <>
                <div className="bg-[#f9f7f4] border border-line p-[0.6rem] text-[0.84rem]">
                  <p className="m-0 text-ink-soft">Editing <strong className="text-ink">{activeTarget}</strong></p>
                  {invalidTargets.includes(activeTarget) && <p className="m-0 mt-[0.2rem] text-[#9b2d2d] font-medium">Required fields are missing for this target.</p>}
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
          <button type="button" disabled={batchRedeployConfirm.running || invalidTargets.length > 0} className="bg-[#d86b6b] border-[#be5252] text-white" onClick={submitBatchRedeployConfirm}>
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
      confirmClass={batchPowerConfirm.action === 'power-on' ? 'bg-brand border-brand-strong text-white' : 'bg-[#d86b6b] border-[#be5252] text-white'}
      confirmLabel={batchPowerConfirm.running ? (batchPowerConfirm.action === 'power-on' ? 'Powering On...' : 'Powering Off...') : (batchPowerConfirm.action === 'power-on' ? 'Power On' : 'Power Off')}
    />
  )
}

function BatchDeleteDialog({ batchDeleteConfirm, setBatchDeleteConfirm, submitBatchDeleteConfirm }: MachineDialogsProps) {
  if (!batchDeleteConfirm.open) return null
  return (
    <ConfirmTargetsDialog
      title="Confirm Batch Delete"
      titleClass="text-[#9b2d2d]"
      targets={batchDeleteConfirm.targets}
      running={batchDeleteConfirm.running}
      onClose={() => setBatchDeleteConfirm(initialBatchDeleteConfirmState)}
      onConfirm={submitBatchDeleteConfirm}
      confirmClass="bg-[#d86b6b] border-[#be5252] text-white"
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

function NameCountFields({ preset, setPreset }: { preset: MachineQuickDeployPreset; setPreset: Dispatch<SetStateAction<MachineQuickDeployPreset>> }) {
  return (
    <div className="grid grid-cols-1 sm:grid-cols-[minmax(0,1fr)_130px] gap-[0.55rem]">
      <label className="text-[0.84rem] min-w-0">Name<input required value={preset.name} onChange={(e) => setPreset((current) => ({ ...current, name: e.target.value }))} placeholder="e.g. node" /></label>
      <label className="text-[0.84rem] min-w-0">Count<input type="number" min="1" value={preset.count} onChange={(e) => setPreset((current) => ({ ...current, count: e.target.value }))} /></label>
    </div>
  )
}

function PowerPresetFields({ preset, setPreset }: { preset: MachineQuickDeployPreset; setPreset: Dispatch<SetStateAction<MachineQuickDeployPreset>> }) {
  return (
    <>
      <label className="text-[0.84rem]">
        Power Control Type
        <select required value={preset.powerType} onChange={(e) => setPreset((current) => ({ ...current, powerType: e.target.value as PowerType }))}>
          <option value="manual">Manual</option>
          <option value="ipmi">IPMI</option>
          <option value="webhook">Webhook</option>
          <option value="wol">Wake-on-LAN</option>
        </select>
      </label>
      {preset.powerType === 'ipmi' && <SecretPowerFields preset={preset} setPreset={setPreset} />}
      {preset.powerType === 'webhook' && <WebhookPresetFields preset={preset} setPreset={setPreset} />}
      {preset.powerType === 'wol' && <label className="text-[0.84rem]">Wake MAC <span className="text-ink-soft">(defaults to machine MAC)</span><input value={preset.wolMAC} onChange={(e) => setPreset((current) => ({ ...current, wolMAC: e.target.value }))} placeholder={preset.mac || 'aa:bb:cc:dd:ee:ff'} /></label>}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
        <label className="text-[0.84rem] min-w-0">Arch<select value={preset.arch} onChange={(e) => setPreset((current) => ({ ...current, arch: e.target.value }))}><option value="amd64">amd64</option><option value="arm64">arm64</option></select></label>
        <label className="text-[0.84rem] min-w-0">Firmware<select value={preset.firmware} onChange={(e) => setPreset((current) => ({ ...current, firmware: e.target.value as 'uefi' | 'bios' }))}><option value="uefi">UEFI</option><option value="bios">BIOS</option></select></label>
      </div>
      <label className="text-[0.84rem] flex items-center gap-2 cursor-pointer select-none"><input type="checkbox" checked={preset.isHypervisor} onChange={(e) => setPreset((current) => ({ ...current, isHypervisor: e.target.checked }))} />Register as Hypervisor</label>
      {preset.isHypervisor && <label className="text-[0.84rem]">Bridge Name<input type="text" value={preset.bridgeName} onChange={(e) => setPreset((current) => ({ ...current, bridgeName: e.target.value }))} placeholder="br0" /></label>}
    </>
  )
}

function SecretPowerFields({ preset, setPreset }: { preset: MachineQuickDeployPreset; setPreset: Dispatch<SetStateAction<MachineQuickDeployPreset>> }) {
  return (
    <div className="grid gap-[0.45rem] pl-[0.4rem] border-l-2 border-brand/30">
      <label className="text-[0.84rem]">BMC Host<input required value={preset.ipmiHost} onChange={(e) => setPreset((current) => ({ ...current, ipmiHost: e.target.value }))} placeholder="10.0.0.1" /></label>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.55rem]">
        <label className="text-[0.84rem] min-w-0">Username<input required value={preset.ipmiUsername} onChange={(e) => setPreset((current) => ({ ...current, ipmiUsername: e.target.value }))} placeholder="admin" /></label>
        <label className="text-[0.84rem] min-w-0">Password<input type="password" required value={preset.ipmiPassword} onChange={(e) => setPreset((current) => ({ ...current, ipmiPassword: e.target.value }))} /></label>
      </div>
      <p className="m-0 text-[0.78rem] text-ink-soft">IPMI password is used for this deploy only and is not saved in localStorage.</p>
    </div>
  )
}

function WebhookPresetFields({ preset, setPreset }: { preset: MachineQuickDeployPreset; setPreset: Dispatch<SetStateAction<MachineQuickDeployPreset>> }) {
  return (
    <div className="grid gap-[0.45rem] pl-[0.4rem] border-l-2 border-brand/30">
      <label className="text-[0.84rem]">Power On URL<input required value={preset.webhookOnURL} onChange={(e) => setPreset((current) => ({ ...current, webhookOnURL: e.target.value }))} placeholder="https://..." /></label>
      <label className="text-[0.84rem]">Power Off URL<input required value={preset.webhookOffURL} onChange={(e) => setPreset((current) => ({ ...current, webhookOffURL: e.target.value }))} placeholder="https://..." /></label>
      <label className="text-[0.84rem]">Status URL <span className="text-ink-soft">(optional)</span><input value={preset.webhookStatusURL} onChange={(e) => setPreset((current) => ({ ...current, webhookStatusURL: e.target.value }))} placeholder="https://..." /></label>
      <label className="text-[0.84rem]">Boot Order URL <span className="text-ink-soft">(optional, BIOS deploy)</span><input value={preset.webhookBootOrderURL} onChange={(e) => setPreset((current) => ({ ...current, webhookBootOrderURL: e.target.value }))} placeholder="https://..." /></label>
    </div>
  )
}

function ImagePresetFields({ preset, setPreset, osImages }: { preset: MachineQuickDeployPreset; setPreset: Dispatch<SetStateAction<MachineQuickDeployPreset>>; osImages: OSImage[] }) {
  return (
    <label className="text-[0.84rem]">
      OS Image
      <select value={preset.imageRef} onChange={(e) => {
        const selected = osImages.find((img) => img.name === e.target.value)
        setPreset((current) => ({ ...current, imageRef: e.target.value, osFamily: selected?.osFamily || '', osVersion: selected?.osVersion || '' }))
      }}>
        <option value="">Select...</option>
        {osImages.filter((img) => supportsDeploymentTarget(img, 'baremetal')).map((img) => <option key={img.name} value={img.name}>{img.name} ({img.osFamily} {img.osVersion}{img.variant ? ` ${img.variant}` : ''})</option>)}
      </select>
    </label>
  )
}

function NetworkPresetFields({ preset, setPreset, subnets }: { preset: MachineQuickDeployPreset; setPreset: Dispatch<SetStateAction<MachineQuickDeployPreset>>; subnets: Subnet[] }) {
  return (
    <fieldset className="border border-line rounded p-0 m-0">
      <legend className="text-[0.84rem] font-medium px-[0.4rem] ml-[0.3rem]">Network</legend>
      <div className="grid gap-[0.45rem] p-[0.7rem]">
        <label className="text-[0.84rem]">
          Subnet {subnets.length === 0 && <span className="text-ink-soft">(optional)</span>}
          <select value={preset.subnetRef} onChange={(e) => {
            const subnetName = e.target.value
            const subnet = subnets.find((item) => item.name === subnetName)
            setPreset((current) => ({ ...current, subnetRef: subnetName, ipAssignment: subnetName ? current.ipAssignment : 'dhcp', staticIP: subnetName ? current.staticIP : '', domain: subnet?.spec.domainName || '' }))
          }}>
            <option value="">None</option>
            {subnets.map((subnet) => <option key={subnet.name} value={subnet.name}>{subnet.name} ({subnet.spec.cidr})</option>)}
          </select>
        </label>
        {preset.subnetRef && (
          <>
            <div className="flex items-center gap-[0.8rem] text-[0.84rem]">
              <span>IP Assignment:</span>
              <label className="flex items-center gap-1 cursor-pointer"><input type="radio" name="machine-quick-deploy-ip-assignment" checked={preset.ipAssignment === 'dhcp'} onChange={() => setPreset((current) => ({ ...current, ipAssignment: 'dhcp' }))} />DHCP</label>
              <label className="flex items-center gap-1 cursor-pointer"><input type="radio" name="machine-quick-deploy-ip-assignment" checked={preset.ipAssignment === 'static'} onChange={() => setPreset((current) => ({ ...current, ipAssignment: 'static' }))} />Static</label>
            </div>
            {preset.ipAssignment === 'static' && <label className="text-[0.84rem]">Static IP<input value={preset.staticIP} onChange={(e) => setPreset((current) => ({ ...current, staticIP: e.target.value }))} placeholder="e.g. 10.0.0.50" /></label>}
            <label className="text-[0.84rem]">Domain<input value={preset.domain} onChange={(e) => setPreset((current) => ({ ...current, domain: e.target.value }))} placeholder="e.g. example.local" /></label>
          </>
        )}
      </div>
    </fieldset>
  )
}

function SSHPresetFields({ preset, setPreset, sshKeys, onToggleSSHKeyRef }: { preset: MachineQuickDeployPreset; setPreset: Dispatch<SetStateAction<MachineQuickDeployPreset>>; sshKeys: SSHKey[]; onToggleSSHKeyRef: (ref: string) => void }) {
  return (
    <fieldset className="border border-line rounded p-0 m-0">
      <legend className="text-[0.84rem] font-medium px-[0.4rem] ml-[0.3rem]">SSH Access</legend>
      <div className="grid gap-[0.45rem] p-[0.7rem]">
        {sshKeys.length === 0 && <p className="m-0 text-[0.78rem] text-ink-soft">No SSH keys available.</p>}
        {sshKeys.map((key) => <label key={key.name} className="flex items-center gap-[0.45rem] text-[0.84rem] cursor-pointer"><input type="checkbox" className="w-[0.95rem] h-[0.95rem] m-0 accent-[#2b7a78]" checked={preset.sshKeyRefs.includes(key.name)} onChange={() => onToggleSSHKeyRef(key.name)} />{key.name}</label>)}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
          <label className="text-[0.84rem] min-w-0">Login Username<input value={preset.loginUserUsername} onChange={(e) => setPreset((current) => ({ ...current, loginUserUsername: e.target.value }))} placeholder="e.g. ubuntu" /></label>
          <label className="text-[0.84rem] min-w-0">Login Password<input type="password" value={preset.loginUserPassword} onChange={(e) => setPreset((current) => ({ ...current, loginUserPassword: e.target.value }))} /></label>
        </div>
        <p className="m-0 text-[0.78rem] text-ink-soft">Login password is used for this deploy only and is not saved in localStorage.</p>
      </div>
    </fieldset>
  )
}

function RedeployTargetList({ confirm, activeTarget, setConfirm }: { confirm: BatchRedeployConfirmState; activeTarget: string; setConfirm: Dispatch<SetStateAction<BatchRedeployConfirmState>> }) {
  return (
    <div className="border border-line bg-[#f9f7f4] p-[0.55rem] max-h-[68vh] overflow-auto">
      <p className="m-0 mb-[0.45rem] text-ink-soft text-[0.78rem]">Targets ({confirm.targets.length})</p>
      <div className="grid gap-[0.35rem]">
        {confirm.targets.map((target) => {
          const status = confirm.status[target]?.state ?? 'pending'
          return (
            <button key={target} type="button" className={clsx('w-full text-left border border-line shadow-none px-[0.55rem] py-[0.48rem] bg-white hover:bg-[#f3efe8]', activeTarget === target && 'border-brand bg-[rgba(43,122,120,0.08)]')} onClick={() => setConfirm((current) => ({ ...current, activeTarget: target }))}>
              <span className="block font-medium break-anywhere">{target}</span>
              <span className={clsx('mt-[0.18rem] inline-flex text-[0.68rem] font-medium px-[0.35rem] py-[0.05rem] rounded-sm border', status === 'succeeded' && 'bg-[#e9f6ec] text-[#25633a] border-[#b8dec3]', status === 'failed' && 'bg-[#fff0ee] text-[#9b2d2d] border-[#ecc1ba]', status === 'running' && 'bg-[#eef5ff] text-[#265f9b] border-[#bfd5ee]', status === 'pending' && 'bg-[#f3f0ea] text-ink-soft border-[#e0dbd2]')}>{status}</span>
              {confirm.status[target]?.error && <span className="block mt-[0.2rem] text-[#9b2d2d] text-[0.72rem] leading-tight">{confirm.status[target]?.error}</span>}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function ConfirmTargetsDialog({ title, titleClass, targets, running, onClose, onConfirm, confirmClass, confirmLabel }: { title: string; titleClass?: string; targets: string[]; running: boolean; onClose: () => void; onConfirm: () => void; confirmClass: string; confirmLabel: string }) {
  return (
    <ModalOverlay onBackdropClick={() => { if (!running) onClose() }}>
      <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
        <h3 className={clsx('text-[1.2rem]', titleClass)}>{title}</h3>
        <p className="m-0 text-ink-soft text-[0.84rem]">Target machines ({targets.length}):</p>
        <div className="max-h-[180px] overflow-auto border border-line p-[0.55rem] bg-[#f9f7f4]"><ul className="m-0 pl-[1.1rem]">{targets.map((target) => <li key={target}><code>{target}</code></li>)}</ul></div>
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
