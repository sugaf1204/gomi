import clsx from 'clsx'
import { useState, useEffect } from 'react'
import type { FormEvent } from 'react'
import type { SubnetFormState } from '../../app-types'
import { formatDate } from '../../lib/formatters'
import type { Subnet } from '../../types'
import { ModalOverlay } from '../ui/ModalOverlay'

export type NetworkViewProps = {
  subnetFormOpen: boolean
  onToggleSubnetForm: () => void
  subnetForm: SubnetFormState
  onSubnetFormChange: (field: keyof SubnetFormState, value: string) => void
  onCreateSubnet: (e: FormEvent) => void | Promise<void>
  subnets: Subnet[]
  selectedSubnet: string
  onSelectSubnet: (name: string) => void
  onDeleteSubnet: (name: string) => void | Promise<void>
  onUpdateSubnet: (name: string, spec: Subnet['spec']) => void | Promise<void>
  selectedSubnetData: Subnet | null
}

const LEASE_TIME_PRESETS = [
  { label: '30 min', value: 1800 },
  { label: '1 hour', value: 3600 },
  { label: '12 hours', value: 43200 },
  { label: '24 hours', value: 86400 },
  { label: '7 days', value: 604800 },
  { label: 'Custom', value: -1 },
] as const

function formatLeaseTime(seconds: number | undefined): string {
  if (!seconds || seconds === 0) return '1 hour (default)'
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${seconds / 60} min`
  if (seconds < 86400) return `${seconds / 3600} hours`
  return `${seconds / 86400} days`
}

type EditForm = {
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

export function NetworkView({
  subnetFormOpen,
  onToggleSubnetForm,
  subnetForm,
  onSubnetFormChange,
  onCreateSubnet,
  subnets,
  selectedSubnet,
  onSelectSubnet,
  onDeleteSubnet,
  onUpdateSubnet,
  selectedSubnetData
}: NetworkViewProps) {
  const [editOpen, setEditOpen] = useState(false)
  const [editForm, setEditForm] = useState<EditForm>({
    cidr: '',
    defaultGateway: '',
    dnsServers: '',
    dnsSearchDomains: '',
    vlanId: '',
    pxeInterface: '',
    leaseTime: '',
    domainName: '',
    ntpServers: ''
  })
  const [leaseTimePreset, setLeaseTimePreset] = useState<number>(0)
  const [pxeRangeStart, setPxeRangeStart] = useState('')
  const [pxeRangeEnd, setPxeRangeEnd] = useState('')
  const [deleteConfirm, setDeleteConfirm] = useState<{ open: boolean; name: string }>({ open: false, name: '' })

  useEffect(() => {
    if (!subnetFormOpen) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') { onToggleSubnetForm() }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [subnetFormOpen, onToggleSubnetForm])

  useEffect(() => {
    if (!editOpen) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') { setEditOpen(false) }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [editOpen])

  function openEdit() {
    if (!selectedSubnetData) return
    const lt = selectedSubnetData.spec.leaseTime ?? 0
    const matchedPreset = LEASE_TIME_PRESETS.find((p) => p.value === lt)
    setEditForm({
      cidr: selectedSubnetData.spec.cidr,
      defaultGateway: selectedSubnetData.spec.defaultGateway ?? '',
      dnsServers: selectedSubnetData.spec.dnsServers?.join(', ') ?? '',
      dnsSearchDomains: selectedSubnetData.spec.dnsSearchDomains?.join(', ') ?? '',
      vlanId: selectedSubnetData.spec.vlanId != null ? String(selectedSubnetData.spec.vlanId) : '',
      pxeInterface: selectedSubnetData.spec.pxeInterface ?? '',
      leaseTime: lt > 0 ? String(lt) : '',
      domainName: selectedSubnetData.spec.domainName ?? '',
      ntpServers: selectedSubnetData.spec.ntpServers?.join(', ') ?? ''
    })
    setLeaseTimePreset(matchedPreset ? matchedPreset.value : lt > 0 ? -1 : 0)
    setPxeRangeStart(selectedSubnetData.spec.pxeAddressRange?.start ?? '')
    setPxeRangeEnd(selectedSubnetData.spec.pxeAddressRange?.end ?? '')
    setEditOpen(true)
  }

  async function handleSaveEdit(e: FormEvent) {
    e.preventDefault()
    if (!selectedSubnetData) return
    const spec: Subnet['spec'] = {
      cidr: editForm.cidr,
      defaultGateway: editForm.defaultGateway || undefined,
      dnsServers: editForm.dnsServers ? editForm.dnsServers.split(',').map((s) => s.trim()).filter(Boolean) : undefined,
      dnsSearchDomains: editForm.dnsSearchDomains ? editForm.dnsSearchDomains.split(',').map((s) => s.trim()).filter(Boolean) : undefined,
      vlanId: editForm.vlanId ? Number(editForm.vlanId) : undefined,
      pxeInterface: editForm.pxeInterface || undefined,
      pxeAddressRange: pxeRangeStart && pxeRangeEnd ? { start: pxeRangeStart, end: pxeRangeEnd } : undefined,
      reservedRanges: selectedSubnetData.spec.reservedRanges,
      leaseTime: editForm.leaseTime ? Number(editForm.leaseTime) : undefined,
      domainName: editForm.domainName || undefined,
      ntpServers: editForm.ntpServers ? editForm.ntpServers.split(',').map((s) => s.trim()).filter(Boolean) : undefined
    }
    await onUpdateSubnet(selectedSubnetData.name, spec)
    setEditOpen(false)
  }

  return (
    <>
      {subnetFormOpen && (
        <ModalOverlay onBackdropClick={onToggleSubnetForm}>
          <div className="w-[min(480px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">Create Subnet</h3>
              <button
                aria-label="Close"
                className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                onClick={onToggleSubnetForm}
              >×</button>
            </div>
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
                <button type="button" onClick={onToggleSubnetForm}>Cancel</button>
                <button type="submit" className="bg-brand border-brand-strong text-white">Create</button>
              </div>
            </form>
          </div>
        </ModalOverlay>
      )}

      {editOpen && selectedSubnetData && (
        <ModalOverlay onBackdropClick={() => { setEditOpen(false) }}>
          <div className="w-[min(480px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">Edit Subnet: {selectedSubnetData.name}</h3>
              <button
                aria-label="Close"
                className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                onClick={() => setEditOpen(false)}
              >×</button>
            </div>
            <form className="grid gap-[0.55rem] max-h-[70vh] overflow-y-auto" onSubmit={(e) => void handleSaveEdit(e)}>
              <label className="text-[0.84rem]">
                CIDR
                <input required value={editForm.cidr} onChange={(e) => setEditForm((f) => ({ ...f, cidr: e.target.value }))} placeholder="e.g. 10.0.0.0/24" />
              </label>
              <label className="text-[0.84rem]">
                Default Gateway
                <input value={editForm.defaultGateway} onChange={(e) => setEditForm((f) => ({ ...f, defaultGateway: e.target.value }))} placeholder="e.g. 10.0.0.1" />
              </label>
              <label className="text-[0.84rem]">
                PXE Interface
                <input value={editForm.pxeInterface} onChange={(e) => setEditForm((f) => ({ ...f, pxeInterface: e.target.value }))} placeholder="e.g. eth0" />
              </label>
              <div className="grid grid-cols-2 gap-[0.45rem]">
                <label className="text-[0.84rem]">
                  PXE Range Start
                  <input value={pxeRangeStart} onChange={(e) => setPxeRangeStart(e.target.value)} placeholder="e.g. 192.168.2.100" />
                </label>
                <label className="text-[0.84rem]">
                  PXE Range End
                  <input value={pxeRangeEnd} onChange={(e) => setPxeRangeEnd(e.target.value)} placeholder="e.g. 192.168.2.200" />
                </label>
              </div>
              <label className="text-[0.84rem]">
                VLAN ID
                <input type="number" value={editForm.vlanId} onChange={(e) => setEditForm((f) => ({ ...f, vlanId: e.target.value }))} placeholder="optional" />
              </label>

              <div className="border-t border-line pt-[0.5rem] mt-[0.2rem]">
                <span className="text-[0.78rem] font-medium text-ink-soft uppercase tracking-wide">DHCP Options</span>
              </div>
              <label className="text-[0.84rem]">
                DNS Servers
                <input value={editForm.dnsServers} onChange={(e) => setEditForm((f) => ({ ...f, dnsServers: e.target.value }))} placeholder="e.g. 8.8.8.8, 8.8.4.4" />
              </label>
              <label className="text-[0.84rem]">
                DNS Search Domains
                <input value={editForm.dnsSearchDomains} onChange={(e) => setEditForm((f) => ({ ...f, dnsSearchDomains: e.target.value }))} placeholder="e.g. example.com, local" />
              </label>
              <label className="text-[0.84rem]">
                Domain Name
                <input value={editForm.domainName} onChange={(e) => setEditForm((f) => ({ ...f, domainName: e.target.value }))} placeholder="e.g. lab.local" />
              </label>
              <label className="text-[0.84rem]">
                NTP Servers
                <input value={editForm.ntpServers} onChange={(e) => setEditForm((f) => ({ ...f, ntpServers: e.target.value }))} placeholder="e.g. 192.168.2.1, 10.0.0.1" />
              </label>
              <label className="text-[0.84rem]">
                Lease Time
                <div className="flex gap-[0.35rem] items-center">
                  <select
                    className="flex-1"
                    value={leaseTimePreset}
                    onChange={(e) => {
                      const v = Number(e.target.value)
                      setLeaseTimePreset(v)
                      if (v > 0) {
                        setEditForm((f) => ({ ...f, leaseTime: String(v) }))
                      } else if (v === 0) {
                        setEditForm((f) => ({ ...f, leaseTime: '' }))
                      }
                    }}
                  >
                    <option value={0}>Default (1 hour)</option>
                    {LEASE_TIME_PRESETS.map((p) => (
                      <option key={p.value} value={p.value}>{p.label}</option>
                    ))}
                  </select>
                  {leaseTimePreset === -1 && (
                    <input
                      type="number"
                      className="w-[6rem]"
                      value={editForm.leaseTime}
                      onChange={(e) => setEditForm((f) => ({ ...f, leaseTime: e.target.value }))}
                      placeholder="seconds"
                      min={60}
                      max={604800}
                    />
                  )}
                </div>
              </label>
              <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
                <button type="button" onClick={() => setEditOpen(false)}>Cancel</button>
                <button type="submit" className="bg-brand border-brand-strong text-white">Save</button>
              </div>
            </form>
          </div>
        </ModalOverlay>
      )}

      <section className="min-h-0 grid grid-cols-[minmax(0,1fr)_320px] gap-[0.9rem] items-start lg:grid-cols-[minmax(0,1fr)_320px] max-lg:grid-cols-1">
        <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.65rem] self-start">
          <div className="flex justify-between items-center gap-4 max-sm:flex-col max-sm:items-start">
            <h2 className="text-[1.4rem]">Subnets</h2>
            <button className="bg-brand border-brand-strong text-white py-[0.35rem] px-[0.55rem] text-[0.82rem]" onClick={onToggleSubnetForm}>
              Create Subnet
            </button>
          </div>

          <div className="overflow-auto max-h-[58vh]">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>CIDR</th>
                  <th>Gateway</th>
                  <th>VLAN</th>
                  <th>PXE Interface</th>
                  <th>DNS</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {subnets.map((subnet) => (
                  <tr
                    key={subnet.name}
                    className={clsx(selectedSubnet === subnet.name && 'bg-[#eef6f6]')}
                    onClick={() => onSelectSubnet(subnet.name)}
                  >
                    <td>{subnet.name}</td>
                    <td><code>{subnet.spec.cidr}</code></td>
                    <td>{subnet.spec.defaultGateway || '-'}</td>
                    <td>{subnet.spec.vlanId ?? '-'}</td>
                    <td>{subnet.spec.pxeInterface || '-'}</td>
                    <td>{subnet.spec.dnsServers?.join(', ') || '-'}</td>
                    <td>
                      <button
                        className="bg-[#d86b6b] border-[#be5252] text-white py-[0.28rem] px-[0.55rem] text-[0.78rem]"
                        onClick={(e) => {
                          e.stopPropagation()
                          setDeleteConfirm({ open: true, name: subnet.name })
                        }}
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {subnets.length === 0 && <p className="m-0 text-ink-soft">No subnets found</p>}
          </div>
        </section>

        <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
          <div className="flex justify-between items-center mb-[0.58rem]">
            <h3 className="text-[1.05rem]">Subnet Details</h3>
            {selectedSubnetData && (
              <button
                className="bg-brand border-brand-strong text-white py-[0.28rem] px-[0.55rem] text-[0.78rem]"
                onClick={openEdit}
              >
                Edit Subnet
              </button>
            )}
          </div>
          {!selectedSubnetData && <p className="m-0 text-ink-soft">Select a subnet to view details</p>}
          {selectedSubnetData && (
            <div className="grid gap-[0.65rem]">
              <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                <dt className="text-ink-soft text-[0.84rem]">Name</dt><dd className="m-0">{selectedSubnetData.name}</dd>
                <dt className="text-ink-soft text-[0.84rem]">CIDR</dt><dd className="m-0">{selectedSubnetData.spec.cidr}</dd>
                <dt className="text-ink-soft text-[0.84rem]">Default Gateway</dt><dd className="m-0">{selectedSubnetData.spec.defaultGateway || '-'}</dd>
                <dt className="text-ink-soft text-[0.84rem]">PXE Interface</dt><dd className="m-0">{selectedSubnetData.spec.pxeInterface || '-'}</dd>
                <dt className="text-ink-soft text-[0.84rem]">PXE Range</dt>
                <dd className="m-0">
                  {selectedSubnetData.spec.pxeAddressRange
                    ? `${selectedSubnetData.spec.pxeAddressRange.start} - ${selectedSubnetData.spec.pxeAddressRange.end}`
                    : '-'}
                </dd>
                <dt className="text-ink-soft text-[0.84rem]">VLAN ID</dt><dd className="m-0">{selectedSubnetData.spec.vlanId ?? '-'}</dd>
                <dt className="text-ink-soft text-[0.84rem]">Reserved Ranges</dt>
                <dd className="m-0">
                  {selectedSubnetData.spec.reservedRanges && selectedSubnetData.spec.reservedRanges.length > 0
                    ? selectedSubnetData.spec.reservedRanges.map((r) => `${r.start}-${r.end}`).join(', ')
                    : '-'}
                </dd>
                <dt className="text-ink-soft text-[0.84rem]">Created</dt><dd className="m-0">{formatDate(selectedSubnetData.createdAt)}</dd>
                <dt className="text-ink-soft text-[0.84rem]">Updated</dt><dd className="m-0">{formatDate(selectedSubnetData.updatedAt)}</dd>
              </dl>

              <div className="border-t border-line pt-[0.5rem]">
                <h4 className="text-[0.88rem] m-0 mb-[0.4rem]">DHCP Options</h4>
                <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                  <dt className="text-ink-soft text-[0.84rem]">Lease Time</dt>
                  <dd className="m-0">{formatLeaseTime(selectedSubnetData.spec.leaseTime)}</dd>
                  <dt className="text-ink-soft text-[0.84rem]">DNS Servers</dt>
                  <dd className="m-0">{selectedSubnetData.spec.dnsServers?.join(', ') || '-'}</dd>
                  <dt className="text-ink-soft text-[0.84rem]">Search Domains</dt>
                  <dd className="m-0">{selectedSubnetData.spec.dnsSearchDomains?.join(', ') || '-'}</dd>
                  <dt className="text-ink-soft text-[0.84rem]">Domain Name</dt>
                  <dd className="m-0">{selectedSubnetData.spec.domainName || '-'}</dd>
                  <dt className="text-ink-soft text-[0.84rem]">NTP Servers</dt>
                  <dd className="m-0">{selectedSubnetData.spec.ntpServers?.join(', ') || '-'}</dd>
                </dl>
              </div>
            </div>
          )}
        </section>
      </section>

      {deleteConfirm.open && (
        <ModalOverlay onBackdropClick={() => setDeleteConfirm({ open: false, name: '' })}>
          <div className="w-[min(400px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[0.95rem] grid gap-[0.6rem]">
            <h3 className="text-[1.2rem] text-[#9b2d2d]">Delete Subnet</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">Are you sure you want to delete this subnet?</p>
            <div className="border border-line bg-[#f9f7f4] p-[0.55rem]">
              <code>{deleteConfirm.name}</code>
            </div>
            <div className="flex justify-end gap-[0.45rem]">
              <button onClick={() => setDeleteConfirm({ open: false, name: '' })}>Cancel</button>
              <button
                className="bg-[#d86b6b] border-[#be5252] text-white"
                onClick={() => { void onDeleteSubnet(deleteConfirm.name); setDeleteConfirm({ open: false, name: '' }) }}
              >
                Delete
              </button>
            </div>
          </div>
        </ModalOverlay>
      )}
    </>
  )
}
