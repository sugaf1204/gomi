import { supportsDeploymentTarget } from '../../../lib/osImages'
import type { CloudInitTemplate, OSImage, PowerType, SSHKey, Subnet } from '../../../types'
import { SSHAccessFieldset } from '../SSHAccessFieldset'
import type { CloudInitInputMode, MachineFormState, UpdateMachineForm } from './machineFormState'

type MachineSpecFieldsProps = {
  formState: MachineFormState
  updateForm: UpdateMachineForm
  radioName: string
  osImages: OSImage[]
  cloudInits: CloudInitTemplate[]
  sshKeys: SSHKey[]
  subnets: Subnet[]
  onRefresh: () => void | Promise<void>
}

export function MachineSpecFields({ formState, updateForm, radioName, osImages, cloudInits, sshKeys, subnets, onRefresh }: MachineSpecFieldsProps) {
  return (
    <>
      <label className="text-[0.84rem]">
        Hostname
        <input required value={formState.hostname} onChange={(e) => updateForm((current) => ({ ...current, hostname: e.target.value }))} placeholder="e.g. node1" />
      </label>
      <label className="text-[0.84rem]">
        MAC Address
        <input required value={formState.mac} onChange={(e) => updateForm((current) => ({ ...current, mac: e.target.value }))} placeholder="aa:bb:cc:dd:ee:ff" />
      </label>
      <label className="text-[0.84rem]">
        Power Control Type
        <select required value={formState.powerType} onChange={(e) => updateForm((current) => ({ ...current, powerType: e.target.value as PowerType }))}>
          <option value="manual">Manual</option>
          <option value="ipmi">IPMI</option>
          <option value="webhook">Webhook</option>
          <option value="wol">Wake-on-LAN</option>
        </select>
      </label>
      {formState.powerType === 'ipmi' && <IPMIFields formState={formState} updateForm={updateForm} />}
      {formState.powerType === 'webhook' && <WebhookFields formState={formState} updateForm={updateForm} />}
      {formState.powerType === 'wol' && (
        <div className="grid gap-[0.45rem] pl-[0.4rem] border-l-2 border-brand/30">
          <label className="text-[0.84rem]">
            Wake MAC <span className="text-ink-soft">(defaults to machine MAC)</span>
            <input value={formState.wolMAC} onChange={(e) => updateForm((current) => ({ ...current, wolMAC: e.target.value }))} placeholder={formState.mac || 'aa:bb:cc:dd:ee:ff'} />
          </label>
        </div>
      )}
      <div className="grid grid-cols-2 gap-[0.55rem]">
        <label className="text-[0.84rem]">
          Arch
          <select value={formState.arch} onChange={(e) => updateForm((current) => ({ ...current, arch: e.target.value }))}>
            <option value="amd64">amd64</option>
            <option value="arm64">arm64</option>
          </select>
        </label>
        <label className="text-[0.84rem]">
          Firmware
          <select value={formState.firmware} onChange={(e) => updateForm((current) => ({ ...current, firmware: e.target.value as 'uefi' | 'bios' }))}>
            <option value="uefi">UEFI</option>
            <option value="bios">BIOS</option>
          </select>
        </label>
      </div>
      <label className="text-[0.84rem] flex items-center gap-2 cursor-pointer select-none">
        <input type="checkbox" checked={formState.isHypervisor} onChange={(e) => updateForm((current) => ({ ...current, isHypervisor: e.target.checked }))} />
        Register as Hypervisor
      </label>
      {formState.isHypervisor && (
        <label className="text-[0.84rem]">
          Bridge Name
          <input type="text" value={formState.bridgeName} onChange={(e) => updateForm((current) => ({ ...current, bridgeName: e.target.value }))} placeholder="br0" />
        </label>
      )}
      <label className="text-[0.84rem]">
        OS Image
        <select value={formState.imageRef} onChange={(e) => {
          const selected = osImages.find((img) => img.name === e.target.value)
          updateForm((current) => ({ ...current, imageRef: e.target.value, osFamily: selected?.osFamily || '', osVersion: selected?.osVersion || '' }))
        }}>
          <option value="">None</option>
          {osImages.filter((img) => supportsDeploymentTarget(img, 'baremetal')).map((img) => (
            <option key={img.name} value={img.name}>{img.name} ({img.osFamily} {img.osVersion}{img.variant ? ` ${img.variant}` : ''})</option>
          ))}
        </select>
      </label>
      <label className="text-[0.84rem]">
        Target Disk <span className="text-ink-soft">(optional)</span>
        <input value={formState.targetDisk} onChange={(e) => updateForm((current) => ({ ...current, targetDisk: e.target.value }))} placeholder="/dev/disk/by-id/..." />
      </label>
      <CloudInitFields formState={formState} updateForm={updateForm} cloudInits={cloudInits} />
      <SSHAccessFieldset sshKeys={sshKeys} value={formState} onChange={(updater) => updateForm((current) => ({ ...current, ...updater(current) }))} onRefresh={onRefresh} />
      <NetworkFields formState={formState} updateForm={updateForm} radioName={radioName} subnets={subnets} />
    </>
  )
}

function IPMIFields({ formState, updateForm }: { formState: MachineFormState; updateForm: UpdateMachineForm }) {
  return (
    <div className="grid gap-[0.45rem] pl-[0.4rem] border-l-2 border-brand/30">
      <label className="text-[0.84rem]">BMC Host<input required value={formState.ipmiHost} onChange={(e) => updateForm((current) => ({ ...current, ipmiHost: e.target.value }))} placeholder="10.0.0.1" /></label>
      <div className="grid grid-cols-2 gap-[0.55rem]">
        <label className="text-[0.84rem]">Username<input required value={formState.ipmiUsername} onChange={(e) => updateForm((current) => ({ ...current, ipmiUsername: e.target.value }))} placeholder="admin" /></label>
        <label className="text-[0.84rem]">Password<input type="password" required value={formState.ipmiPassword} onChange={(e) => updateForm((current) => ({ ...current, ipmiPassword: e.target.value }))} /></label>
      </div>
    </div>
  )
}

function WebhookFields({ formState, updateForm }: { formState: MachineFormState; updateForm: UpdateMachineForm }) {
  return (
    <div className="grid gap-[0.45rem] pl-[0.4rem] border-l-2 border-brand/30">
      <label className="text-[0.84rem]">Power On URL<input required value={formState.webhookOnURL} onChange={(e) => updateForm((current) => ({ ...current, webhookOnURL: e.target.value }))} placeholder="https://..." /></label>
      <label className="text-[0.84rem]">Power Off URL<input required value={formState.webhookOffURL} onChange={(e) => updateForm((current) => ({ ...current, webhookOffURL: e.target.value }))} placeholder="https://..." /></label>
      <label className="text-[0.84rem]">Status URL <span className="text-ink-soft">(optional)</span><input value={formState.webhookStatusURL} onChange={(e) => updateForm((current) => ({ ...current, webhookStatusURL: e.target.value }))} placeholder="https://..." /></label>
      <label className="text-[0.84rem]">Boot Order URL <span className="text-ink-soft">(optional, BIOS deploy)</span><input value={formState.webhookBootOrderURL} onChange={(e) => updateForm((current) => ({ ...current, webhookBootOrderURL: e.target.value }))} placeholder="https://..." /></label>
    </div>
  )
}

function CloudInitFields({ formState, updateForm, cloudInits }: { formState: MachineFormState; updateForm: UpdateMachineForm; cloudInits: CloudInitTemplate[] }) {
  return (
    <>
      <label className="text-[0.84rem]">
        Cloud-Init <span className="text-ink-soft">(optional)</span>
        <select value={formState.cloudInitMode} onChange={(e) => updateForm((current) => ({ ...current, cloudInitMode: e.target.value as CloudInitInputMode }))}>
          <option value="none">None</option>
          <option value="existing">Use existing template</option>
          <option value="create">Create template inline</option>
        </select>
      </label>
      {formState.cloudInitMode === 'existing' && (
        <label className="text-[0.84rem]">
          Existing Cloud-Init Template
          <select value={formState.cloudInitExistingRef} onChange={(e) => updateForm((current) => ({ ...current, cloudInitExistingRef: e.target.value }))}>
            <option value="">None</option>
            {cloudInits.map((ci) => <option key={ci.name} value={ci.name}>{ci.name}</option>)}
          </select>
        </label>
      )}
      {formState.cloudInitMode === 'existing' && cloudInits.length === 0 && <p className="m-0 text-[0.78rem] text-ink-soft">No Cloud-Init templates available. Switch mode to create one inline.</p>}
      {formState.cloudInitMode === 'create' && (
        <>
          <label className="text-[0.84rem]">Cloud-Init Template Name<input required value={formState.cloudInitTemplateName} onChange={(e) => updateForm((current) => ({ ...current, cloudInitTemplateName: e.target.value }))} placeholder="e.g. ci-node1" /></label>
          <label className="text-[0.84rem]">
            Cloud-Init User Data
            <textarea required className="min-h-[140px] font-mono text-[0.78rem]" value={formState.cloudInitUserData} onChange={(e) => updateForm((current) => ({ ...current, cloudInitUserData: e.target.value }))} placeholder="#cloud-config\nusers:\n  - default" />
          </label>
        </>
      )}
    </>
  )
}

function NetworkFields({ formState, updateForm, radioName, subnets }: { formState: MachineFormState; updateForm: UpdateMachineForm; radioName: string; subnets: Subnet[] }) {
  return (
    <>
      <label className="text-[0.84rem]">
        Subnet {subnets.length === 0 && <span className="text-ink-soft">(optional)</span>}
        <select value={formState.subnetRef} onChange={(e) => {
          const subnetName = e.target.value
          const subnet = subnets.find((item) => item.name === subnetName)
          updateForm((current) => ({ ...current, subnetRef: subnetName, ipAssignment: subnetName ? current.ipAssignment : 'dhcp', staticIP: subnetName ? current.staticIP : '', domain: subnet?.spec.domainName || '' }))
        }}>
          {subnets.length === 0 && <option value="">None</option>}
          {subnets.map((subnet) => <option key={subnet.name} value={subnet.name}>{subnet.name} ({subnet.spec.cidr})</option>)}
        </select>
      </label>
      {formState.subnetRef && (
        <div className="grid gap-[0.45rem] pl-[0.4rem] border-l-2 border-brand/30">
          <div className="flex items-center gap-[0.8rem] text-[0.84rem]">
            <span>IP Assignment:</span>
            <label className="flex items-center gap-1 cursor-pointer"><input type="radio" name={`${radioName}-ip-assignment`} checked={formState.ipAssignment === 'dhcp'} onChange={() => updateForm((current) => ({ ...current, ipAssignment: 'dhcp' }))} />DHCP</label>
            <label className="flex items-center gap-1 cursor-pointer"><input type="radio" name={`${radioName}-ip-assignment`} checked={formState.ipAssignment === 'static'} onChange={() => updateForm((current) => ({ ...current, ipAssignment: 'static' }))} />Static</label>
          </div>
          {formState.ipAssignment === 'static' && <label className="text-[0.84rem]">Static IP<input value={formState.staticIP} onChange={(e) => updateForm((current) => ({ ...current, staticIP: e.target.value }))} placeholder="e.g. 10.0.0.50" /></label>}
          <label className="text-[0.84rem]">Domain<input value={formState.domain} onChange={(e) => updateForm((current) => ({ ...current, domain: e.target.value }))} placeholder="e.g. example.local" /></label>
        </div>
      )}
    </>
  )
}
