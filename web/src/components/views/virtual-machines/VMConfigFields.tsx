import { supportsDeploymentTarget } from '../../../lib/osImages'
import type { CloudInitTemplate, Hypervisor, OSImage, SSHKey, Subnet } from '../../../types'
import { SSHAccessFieldset } from '../SSHAccessFieldset'
import type { CloudInitInputMode, UpdateVMConfigForm, VMConfigForm } from './vmFormState'

type VMConfigFieldsProps = {
  formState: VMConfigForm
  updateForm: UpdateVMConfigForm
  advancedExpanded: boolean
  setAdvancedExpanded: (value: boolean | ((current: boolean) => boolean)) => void
  radioNamePrefix: string
  hypervisors: Hypervisor[]
  vmOSImages: OSImage[]
  cloudInits: CloudInitTemplate[]
  sshKeys: SSHKey[]
  subnets: Subnet[]
  osImageByName: Map<string, OSImage>
  onRefresh: () => void | Promise<void>
  bridgePlaceholder: (formState: VMConfigForm) => string
}

export function VMConfigFields({
  formState,
  updateForm,
  advancedExpanded,
  setAdvancedExpanded,
  radioNamePrefix,
  hypervisors,
  vmOSImages,
  cloudInits,
  sshKeys,
  subnets,
  osImageByName,
  onRefresh,
  bridgePlaceholder
}: VMConfigFieldsProps) {
  const selectedOSImage = formState.osImageRef ? osImageByName.get(formState.osImageRef) : undefined
  const hasUnsupportedSelectedOSImage = Boolean(selectedOSImage && !supportsDeploymentTarget(selectedOSImage, 'vm'))

  return (
    <>
      <label className="text-[0.84rem]">
        Hypervisor
        <select value={formState.hypervisorRef} onChange={(e) => {
          const hvName = e.target.value
          const hv = hypervisors.find((item) => item.name === hvName)
          updateForm((current) => ({ ...current, hypervisorRef: hvName, bridge: hv?.bridgeName || current.bridge }))
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
          <input type="number" required min="1" value={formState.cpuCores} onChange={(e) => updateForm((current) => ({ ...current, cpuCores: e.target.value }))} />
        </label>
        <label className="text-[0.84rem] min-w-0">
          Memory (MB)
          <input type="number" required min="256" value={formState.memoryMB} onChange={(e) => updateForm((current) => ({ ...current, memoryMB: e.target.value }))} />
        </label>
        <label className="text-[0.84rem] min-w-0">
          Disk (GB)
          <input type="number" required min="1" value={formState.diskGB} onChange={(e) => updateForm((current) => ({ ...current, diskGB: e.target.value }))} />
        </label>
      </div>
      <label className="text-[0.84rem]">
        OS Image
        <select required value={formState.osImageRef} onChange={(e) => updateForm((current) => ({ ...current, osImageRef: e.target.value }))}>
          <option value="">Select...</option>
          {hasUnsupportedSelectedOSImage && selectedOSImage && (
            <option value={selectedOSImage.name} disabled>
              {selectedOSImage.name} ({selectedOSImage.osFamily} {selectedOSImage.osVersion}{selectedOSImage.variant ? ` ${selectedOSImage.variant}` : ''}, unsupported for VM)
            </option>
          )}
          {vmOSImages.map((img) => (
            <option key={img.name} value={img.name}>{img.name} ({img.osFamily} {img.osVersion}{img.variant ? ` ${img.variant}` : ''})</option>
          ))}
        </select>
      </label>
      <label className="text-[0.84rem]">
        Cloud-Init
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
            {cloudInits.map((ci) => (
              <option key={ci.name} value={ci.name}>{ci.name}</option>
            ))}
          </select>
        </label>
      )}
      {formState.cloudInitMode === 'existing' && cloudInits.length === 0 && (
        <p className="m-0 text-[0.78rem] text-ink-soft">No Cloud-Init templates available. Switch mode to create one inline.</p>
      )}
      {formState.cloudInitMode === 'create' && (
        <>
          <label className="text-[0.84rem]">
            Cloud-Init Template Name
            <input
              value={formState.cloudInitTemplateName}
              onChange={(e) => updateForm((current) => ({ ...current, cloudInitTemplateName: e.target.value }))}
              placeholder="e.g. vm-inline-template"
            />
          </label>
          <label className="text-[0.84rem]">
            Cloud-Init User Data
            <textarea
              className="min-h-[140px] font-mono text-[0.78rem]"
              value={formState.cloudInitUserData}
              onChange={(e) => updateForm((current) => ({ ...current, cloudInitUserData: e.target.value }))}
              placeholder="#cloud-config\nusers:\n  - default"
            />
          </label>
        </>
      )}
      <SSHAccessFieldset
        sshKeys={sshKeys}
        value={formState}
        onChange={(updater) => updateForm((current) => ({ ...current, ...updater(current) }))}
        onRefresh={onRefresh}
      />
      <fieldset className="border border-line rounded p-0 m-0">
        <legend className="text-[0.84rem] font-medium px-[0.4rem] ml-[0.3rem]">Network</legend>
        <div className="grid gap-[0.45rem] p-[0.7rem]">
          <label className="text-[0.84rem]">
            Subnet
            <select value={formState.subnetRef} onChange={(e) => {
              const subnetName = e.target.value
              const subnet = subnets.find((item) => item.name === subnetName)
              updateForm((current) => ({
                ...current,
                subnetRef: subnetName,
                bridge: subnet?.spec.pxeInterface || ''
              }))
            }}>
              <option value="">None (use default)</option>
              {subnets.map((subnet) => (
                <option key={subnet.name} value={subnet.name}>{subnet.name} ({subnet.spec.cidr})</option>
              ))}
            </select>
          </label>
          <div className="flex items-center gap-[0.8rem] text-[0.84rem]">
            <span>IP Assignment:</span>
            <label className="flex items-center gap-1 cursor-pointer">
              <input type="radio" name={`${radioNamePrefix}-ip-assignment`} checked={formState.ipAssignment === 'dhcp'} onChange={() => updateForm((current) => ({ ...current, ipAssignment: 'dhcp' }))} />
              DHCP
            </label>
            <label className="flex items-center gap-1 cursor-pointer">
              <input type="radio" name={`${radioNamePrefix}-ip-assignment`} checked={formState.ipAssignment === 'static'} onChange={() => updateForm((current) => ({ ...current, ipAssignment: 'static' }))} />
              Static
            </label>
          </div>
          {formState.ipAssignment === 'static' && (
            <label className="text-[0.84rem]">
              Static IP
              <input value={formState.staticIP} onChange={(e) => updateForm((current) => ({ ...current, staticIP: e.target.value }))} placeholder="e.g. 192.168.1.100" />
            </label>
          )}
          <label className="text-[0.84rem]">
            Bridge
            <input value={formState.bridge} onChange={(e) => updateForm((current) => ({ ...current, bridge: e.target.value }))} placeholder={bridgePlaceholder(formState)} />
          </label>
          <label className="text-[0.84rem]">
            Domain
            <input value={formState.domain} onChange={(e) => updateForm((current) => ({ ...current, domain: e.target.value }))} placeholder="e.g. example.local" />
          </label>
        </div>
      </fieldset>
      <div className="border border-line rounded">
        <button
          type="button"
          className="w-full text-left border-0 shadow-none rounded-none px-[0.7rem] py-[0.5rem] text-[0.84rem] font-medium bg-[#f9f7f4] hover:bg-[#f3efe8]"
          onClick={() => setAdvancedExpanded((current) => !current)}
        >
          {advancedExpanded ? '▾' : '▸'} Advanced Options
        </button>
        {advancedExpanded && (
          <div className="grid gap-[0.45rem] p-[0.7rem] border-t border-line">
            <label className="text-[0.84rem] min-w-0">
              CPU Mode
              <select value={formState.cpuMode} onChange={(e) => updateForm((current) => ({ ...current, cpuMode: e.target.value as VMConfigForm['cpuMode'] }))}>
                <option value="">Default</option>
                <option value="host-passthrough">host-passthrough</option>
                <option value="host-model">host-model</option>
                <option value="maximum">maximum</option>
              </select>
            </label>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
              <label className="text-[0.84rem] min-w-0">
                Disk Driver
                <select value={formState.diskDriver} onChange={(e) => updateForm((current) => ({ ...current, diskDriver: e.target.value }))}>
                  <option value="">Default (virtio)</option>
                  <option value="virtio">virtio</option>
                  <option value="scsi">scsi (virtio-scsi)</option>
                </select>
              </label>
              <label className="text-[0.84rem] min-w-0">
                Disk Format
                <select value={formState.diskFormat} onChange={(e) => updateForm((current) => ({ ...current, diskFormat: e.target.value }))}>
                  <option value="">Default (qcow2)</option>
                  <option value="qcow2">qcow2</option>
                </select>
              </label>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
              <label className="text-[0.84rem] min-w-0">
                IO Threads
                <input type="number" min="0" max="16" value={formState.ioThreads} onChange={(e) => updateForm((current) => ({ ...current, ioThreads: e.target.value }))} placeholder="0 (disabled)" />
              </label>
              <label className="text-[0.84rem] min-w-0">
                Net Multiqueue
                <input type="number" min="0" max="16" value={formState.netMultiqueue} onChange={(e) => updateForm((current) => ({ ...current, netMultiqueue: e.target.value }))} placeholder="0 (disabled)" />
              </label>
            </div>
            <label className="text-[0.84rem]">
              CPU Pinning
              <input value={formState.cpuPinning} onChange={(e) => updateForm((current) => ({ ...current, cpuPinning: e.target.value }))} placeholder="e.g. 0:0,1:2,2:4 (vcpu:cpuset)" />
            </label>
          </div>
        )}
      </div>
    </>
  )
}
