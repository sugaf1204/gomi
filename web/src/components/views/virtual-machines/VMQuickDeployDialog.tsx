import type { Dispatch, SetStateAction } from 'react'
import { ModalOverlay } from '../../ui/ModalOverlay'
import type { CloudInitTemplate, Hypervisor, OSImage, SSHKey, Subnet } from '../../../types'
import type { QuickDeployPreset } from './vmFormState'

type VMQuickDeployDialogProps = {
  open: boolean
  preset: QuickDeployPreset
  setPreset: Dispatch<SetStateAction<QuickDeployPreset>>
  quickDeploying: boolean
  hypervisors: Hypervisor[]
  osImages: OSImage[]
  cloudInits: CloudInitTemplate[]
  sshKeys: SSHKey[]
  subnets: Subnet[]
  nextName: (preset: QuickDeployPreset) => string
  isReady: (preset: QuickDeployPreset) => boolean
  onClose: () => void
  onDeploy: () => void
  onToggleCloudInitRef: (ref: string) => void
  onToggleSSHKeyRef: (ref: string) => void
}

export function VMQuickDeployDialog({
  open,
  preset,
  setPreset,
  quickDeploying,
  hypervisors,
  osImages,
  cloudInits,
  sshKeys,
  subnets,
  nextName,
  isReady,
  onClose,
  onDeploy,
  onToggleCloudInitRef,
  onToggleSSHKeyRef
}: VMQuickDeployDialogProps) {
  if (!open) return null

  return (
    <ModalOverlay onBackdropClick={() => { if (!quickDeploying) onClose() }}>
      <div className="w-[min(680px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
        <div className="flex justify-between items-center">
          <div>
            <h3 className="text-[1.2rem]">Quick Deploy Preset</h3>
            <p className="m-0 text-ink-soft text-[0.82rem]">Next VM: <code>{preset.name.trim() ? nextName(preset) : '-'}</code></p>
          </div>
          <button
            aria-label="Close"
            className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
            disabled={quickDeploying}
            onClick={onClose}
          >x</button>
        </div>

        <div className="grid gap-[0.55rem]">
          <div className="grid grid-cols-1 sm:grid-cols-[minmax(0,1fr)_130px] gap-[0.55rem]">
            <label className="text-[0.84rem] min-w-0">
              Name
              <input required value={preset.name} onChange={(e) => setPreset((current) => ({ ...current, name: e.target.value }))} placeholder="e.g. devvm" />
            </label>
            <label className="text-[0.84rem] min-w-0">
              Count
              <input type="number" min="1" value={preset.count} onChange={(e) => setPreset((current) => ({ ...current, count: e.target.value }))} />
            </label>
          </div>

          <label className="text-[0.84rem]">
            Hypervisor
            <select value={preset.hypervisorRef} onChange={(e) => {
              const hvName = e.target.value
              const hv = hypervisors.find((item) => item.name === hvName)
              setPreset((current) => ({ ...current, hypervisorRef: hvName, bridge: hv?.bridgeName || current.bridge }))
            }}>
              <option value="">Auto (lowest usage)</option>
              {hypervisors.map((hv) => (
                <option key={hv.name} value={hv.name}>{hv.name} ({hv.connection.host}){hv.bridgeName ? ` [${hv.bridgeName}]` : ''}</option>
              ))}
            </select>
          </label>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-[0.45rem]">
            <label className="text-[0.84rem] min-w-0">
              CPU Cores
              <input type="number" min="1" value={preset.cpuCores} onChange={(e) => setPreset((current) => ({ ...current, cpuCores: e.target.value }))} />
            </label>
            <label className="text-[0.84rem] min-w-0">
              Memory (MB)
              <input type="number" min="256" value={preset.memoryMB} onChange={(e) => setPreset((current) => ({ ...current, memoryMB: e.target.value }))} />
            </label>
            <label className="text-[0.84rem] min-w-0">
              Disk (GB)
              <input type="number" min="1" value={preset.diskGB} onChange={(e) => setPreset((current) => ({ ...current, diskGB: e.target.value }))} />
            </label>
          </div>

          <label className="text-[0.84rem]">
            OS Image
            <select required value={preset.osImageRef} onChange={(e) => setPreset((current) => ({ ...current, osImageRef: e.target.value }))}>
              <option value="">Select...</option>
              {osImages.map((img) => (
                <option key={img.name} value={img.name}>{img.name} ({img.osFamily} {img.osVersion}{img.variant ? ` ${img.variant}` : ''})</option>
              ))}
            </select>
          </label>

          <fieldset className="border border-line rounded p-0 m-0">
            <legend className="text-[0.84rem] font-medium px-[0.4rem] ml-[0.3rem]">Network</legend>
            <div className="grid gap-[0.45rem] p-[0.7rem]">
              <label className="text-[0.84rem]">
                Subnet
                <select value={preset.subnetRef} onChange={(e) => {
                  const subnetName = e.target.value
                  const subnet = subnets.find((item) => item.name === subnetName)
                  setPreset((current) => ({ ...current, subnetRef: subnetName, bridge: subnet?.spec.pxeInterface || '' }))
                }}>
                  <option value="">None (use default)</option>
                  {subnets.map((subnet) => (
                    <option key={subnet.name} value={subnet.name}>{subnet.name} ({subnet.spec.cidr})</option>
                  ))}
                </select>
              </label>
              <label className="text-[0.84rem]">
                Bridge
                <input value={preset.bridge} onChange={(e) => setPreset((current) => ({ ...current, bridge: e.target.value }))} placeholder="virbr0" />
              </label>
              <p className="m-0 text-[0.78rem] text-ink-soft">Quick Deploy always uses DHCP. Use Create for static IPs.</p>
            </div>
          </fieldset>

          <fieldset className="border border-line rounded p-0 m-0">
            <legend className="text-[0.84rem] font-medium px-[0.4rem] ml-[0.3rem]">Cloud-Init</legend>
            <div className="grid gap-[0.35rem] p-[0.7rem]">
              {cloudInits.length === 0 && <p className="m-0 text-[0.78rem] text-ink-soft">No Cloud-Init templates available.</p>}
              {cloudInits.map((ci) => (
                <label key={ci.name} className="flex items-center gap-[0.45rem] text-[0.84rem] cursor-pointer">
                  <input type="checkbox" className="w-[0.95rem] h-[0.95rem] m-0 accent-[#2b7a78]" checked={preset.cloudInitRefs.includes(ci.name)} onChange={() => onToggleCloudInitRef(ci.name)} />
                  {ci.name}
                </label>
              ))}
            </div>
          </fieldset>

          <fieldset className="border border-line rounded p-0 m-0">
            <legend className="text-[0.84rem] font-medium px-[0.4rem] ml-[0.3rem]">SSH Access</legend>
            <div className="grid gap-[0.45rem] p-[0.7rem]">
              {sshKeys.length === 0 && <p className="m-0 text-[0.78rem] text-ink-soft">No SSH keys available.</p>}
              {sshKeys.map((key) => (
                <label key={key.name} className="flex items-center gap-[0.45rem] text-[0.84rem] cursor-pointer">
                  <input type="checkbox" className="w-[0.95rem] h-[0.95rem] m-0 accent-[#2b7a78]" checked={preset.sshKeyRefs.includes(key.name)} onChange={() => onToggleSSHKeyRef(key.name)} />
                  {key.name}
                </label>
              ))}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
                <label className="text-[0.84rem] min-w-0">
                  Login Username
                  <input value={preset.loginUserUsername} onChange={(e) => setPreset((current) => ({ ...current, loginUserUsername: e.target.value }))} placeholder="e.g. ubuntu" />
                </label>
                <label className="text-[0.84rem] min-w-0">
                  Login Password
                  <input type="password" value={preset.loginUserPassword} onChange={(e) => setPreset((current) => ({ ...current, loginUserPassword: e.target.value }))} />
                </label>
              </div>
            </div>
          </fieldset>

          <div className="flex justify-between gap-[0.45rem] pt-[0.2rem]">
            <button type="button" onClick={() => setPreset((current) => ({ ...current, count: '1' }))} disabled={quickDeploying}>Reset Count</button>
            <div className="flex justify-end gap-[0.45rem]">
              <button type="button" onClick={onClose} disabled={quickDeploying}>Close</button>
              <button
                type="button"
                className="bg-brand border-brand-strong text-white"
                disabled={quickDeploying || !isReady(preset)}
                onClick={() => {
                  onClose()
                  onDeploy()
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
