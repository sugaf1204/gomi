import { useEffect } from 'react'
import type { FormEvent, ReactNode } from 'react'
import type { SubnetFormState } from '../../../app-types'
import type { Subnet } from '../../../types'
import { ModalOverlay } from '../../ui/ModalOverlay'

export const LEASE_TIME_PRESETS = [
  { label: '30 min', value: 1800 },
  { label: '1 hour', value: 3600 },
  { label: '12 hours', value: 43200 },
  { label: '24 hours', value: 86400 },
  { label: '7 days', value: 604800 },
  { label: 'Custom', value: -1 },
] as const

export type EditForm = {
  cidr: string
  defaultGateway: string
  dnsServers: string
  dnsSearchDomains: string
  vlanId: string
  pxeInterface: string
  leaseTime: string
  domainName: string
  ntpServers: string
}

/** Closes the dialog on Escape while it is open. */
function useEscapeKey(open: boolean, onClose: () => void) {
  useEffect(() => {
    if (!open) return
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [open, onClose])
}

function Dialog({ title, width, onClose, children }: { title: ReactNode; width: string; onClose: () => void; children: ReactNode }) {
  return (
    <ModalOverlay onBackdropClick={onClose}>
      <div className={`${width} bg-panel border border-line-strong p-[1.1rem] grid gap-[0.65rem]`}>
        <div className="flex justify-between items-center">
          <h3 className="text-[1.2rem]">{title}</h3>
          <button
            aria-label="Close"
            className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
            onClick={onClose}
          >
            ×
          </button>
        </div>
        {children}
      </div>
    </ModalOverlay>
  )
}

export function CreateSubnetDialog({
  subnetForm,
  onSubnetFormChange,
  onCreateSubnet,
  onClose,
}: {
  subnetForm: SubnetFormState
  onSubnetFormChange: (field: keyof SubnetFormState, value: string) => void
  onCreateSubnet: (e: FormEvent) => void | Promise<void>
  onClose: () => void
}) {
  useEscapeKey(true, onClose)

  return (
    <Dialog title="Create Subnet" width="w-[min(480px,100%)]" onClose={onClose}>
      <form className="grid gap-[0.55rem]" onSubmit={(e) => void onCreateSubnet(e)}>
        <div className="grid grid-cols-2 gap-[0.45rem]">
          <label className="text-[0.84rem]">
            Name
            <input required value={subnetForm.name} onChange={(e) => onSubnetFormChange('name', e.target.value)} placeholder="e.g. lab-vlan100" />
          </label>
          <label className="text-[0.84rem]">
            CIDR
            <input required value={subnetForm.cidr} onChange={(e) => onSubnetFormChange('cidr', e.target.value)} placeholder="e.g. 10.0.0.0/24" />
          </label>
        </div>
        <label className="text-[0.84rem]">
          Default Gateway
          <input value={subnetForm.gateway} onChange={(e) => onSubnetFormChange('gateway', e.target.value)} placeholder="e.g. 10.0.0.1" />
        </label>
        <label className="text-[0.84rem]">
          DNS Servers
          <input value={subnetForm.dnsServers} onChange={(e) => onSubnetFormChange('dnsServers', e.target.value)} placeholder="e.g. 8.8.8.8, 8.8.4.4" />
        </label>
        <label className="text-[0.84rem]">
          VLAN ID
          <input type="number" value={subnetForm.vlanId} onChange={(e) => onSubnetFormChange('vlanId', e.target.value)} placeholder="optional" />
        </label>
        <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
          <button type="button" onClick={onClose}>Cancel</button>
          <button type="submit" className="bg-brand border-brand-strong text-white">Create</button>
        </div>
      </form>
    </Dialog>
  )
}

export function EditSubnetDialog({
  subnet,
  editForm,
  onEditFormChange,
  leaseTimePreset,
  onLeaseTimePresetChange,
  pxeRangeStart,
  onPxeRangeStartChange,
  pxeRangeEnd,
  onPxeRangeEndChange,
  onSave,
  onClose,
}: {
  subnet: Subnet
  editForm: EditForm
  onEditFormChange: (field: keyof EditForm, value: string) => void
  leaseTimePreset: number
  onLeaseTimePresetChange: (value: number) => void
  pxeRangeStart: string
  onPxeRangeStartChange: (value: string) => void
  pxeRangeEnd: string
  onPxeRangeEndChange: (value: string) => void
  onSave: (e: FormEvent) => void | Promise<void>
  onClose: () => void
}) {
  useEscapeKey(true, onClose)

  return (
    <Dialog title={`Edit Subnet: ${subnet.name}`} width="w-[min(480px,100%)]" onClose={onClose}>
      <form className="grid gap-[0.55rem] max-h-[70vh] overflow-y-auto" onSubmit={(e) => void onSave(e)}>
        <label className="text-[0.84rem]">
          CIDR
          <input required value={editForm.cidr} onChange={(e) => onEditFormChange('cidr', e.target.value)} placeholder="e.g. 10.0.0.0/24" />
        </label>
        <label className="text-[0.84rem]">
          Default Gateway
          <input value={editForm.defaultGateway} onChange={(e) => onEditFormChange('defaultGateway', e.target.value)} placeholder="e.g. 10.0.0.1" />
        </label>
        <label className="text-[0.84rem]">
          PXE Interface
          <input value={editForm.pxeInterface} onChange={(e) => onEditFormChange('pxeInterface', e.target.value)} placeholder="e.g. eth0" />
        </label>
        <div className="grid grid-cols-2 gap-[0.45rem]">
          <label className="text-[0.84rem]">
            PXE Range Start
            <input value={pxeRangeStart} onChange={(e) => onPxeRangeStartChange(e.target.value)} placeholder="e.g. 192.168.2.100" />
          </label>
          <label className="text-[0.84rem]">
            PXE Range End
            <input value={pxeRangeEnd} onChange={(e) => onPxeRangeEndChange(e.target.value)} placeholder="e.g. 192.168.2.200" />
          </label>
        </div>
        <label className="text-[0.84rem]">
          VLAN ID
          <input type="number" value={editForm.vlanId} onChange={(e) => onEditFormChange('vlanId', e.target.value)} placeholder="optional" />
        </label>

        <div className="border-t border-line pt-[0.5rem] mt-[0.2rem]">
          <span className="text-[0.78rem] font-medium text-ink-soft uppercase tracking-wide">DHCP Options</span>
        </div>
        <label className="text-[0.84rem]">
          DNS Servers
          <input value={editForm.dnsServers} onChange={(e) => onEditFormChange('dnsServers', e.target.value)} placeholder="e.g. 8.8.8.8, 8.8.4.4" />
        </label>
        <label className="text-[0.84rem]">
          DNS Search Domains
          <input value={editForm.dnsSearchDomains} onChange={(e) => onEditFormChange('dnsSearchDomains', e.target.value)} placeholder="e.g. example.com, local" />
        </label>
        <label className="text-[0.84rem]">
          Domain Name
          <input value={editForm.domainName} onChange={(e) => onEditFormChange('domainName', e.target.value)} placeholder="e.g. lab.local" />
        </label>
        <label className="text-[0.84rem]">
          NTP Servers
          <input value={editForm.ntpServers} onChange={(e) => onEditFormChange('ntpServers', e.target.value)} placeholder="e.g. 192.168.2.1, 10.0.0.1" />
        </label>
        <label className="text-[0.84rem]">
          Lease Time
          <div className="flex gap-[0.35rem] items-center">
            <select
              className="flex-1"
              value={leaseTimePreset}
              onChange={(e) => onLeaseTimePresetChange(Number(e.target.value))}
            >
              <option value={0}>Default (1 hour)</option>
              {LEASE_TIME_PRESETS.map((preset) => (
                <option key={preset.value} value={preset.value}>{preset.label}</option>
              ))}
            </select>
            {leaseTimePreset === -1 && (
              <input
                type="number"
                className="w-[6rem]"
                value={editForm.leaseTime}
                onChange={(e) => onEditFormChange('leaseTime', e.target.value)}
                placeholder="seconds"
                min={60}
                max={604800}
              />
            )}
          </div>
        </label>
        <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
          <button type="button" onClick={onClose}>Cancel</button>
          <button type="submit" className="bg-brand border-brand-strong text-white">Save</button>
        </div>
      </form>
    </Dialog>
  )
}

export function DeleteSubnetDialog({
  name,
  onConfirm,
  onClose,
}: {
  name: string
  onConfirm: () => void
  onClose: () => void
}) {
  useEscapeKey(true, onClose)

  return (
    <ModalOverlay onBackdropClick={onClose}>
      <div className="w-[min(400px,100%)] bg-panel border border-line-strong p-[0.95rem] grid gap-[0.6rem]">
        <h3 className="text-[1.2rem] text-danger">Delete Subnet</h3>
        <p className="m-0 text-ink-soft text-[0.84rem]">Are you sure you want to delete this subnet?</p>
        <div className="border border-line bg-panel-2 p-[0.55rem]">
          <code className="font-mono text-[11.5px]">{name}</code>
        </div>
        <div className="flex justify-end gap-[0.45rem]">
          <button onClick={onClose}>Cancel</button>
          <button className="bg-danger-line border-danger text-white" onClick={onConfirm}>Delete</button>
        </div>
      </div>
    </ModalOverlay>
  )
}
