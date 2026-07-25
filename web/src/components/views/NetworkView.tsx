import { useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import type { SubnetFormState } from '../../app-types'
import { computeAddressSpace } from '../../lib/address-space'
import type { DHCPLease, Machine, Subnet } from '../../types'
import { AddressSpaceRuler } from './network/AddressSpaceRuler'
import { DHCPLeaseTable } from './network/DHCPLeaseTable'
import { SubnetFacts } from './network/SubnetFacts'
import { SubnetTable } from './network/SubnetTable'
import {
  CreateSubnetDialog,
  DeleteSubnetDialog,
  EditSubnetDialog,
  LEASE_TIME_PRESETS,
  type EditForm,
} from './network/SubnetDialogs'

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
  dhcpLeases: DHCPLease[]
  machines: Machine[]
}

const EMPTY_EDIT_FORM: EditForm = {
  cidr: '',
  defaultGateway: '',
  dnsServers: '',
  dnsSearchDomains: '',
  vlanId: '',
  pxeInterface: '',
  leaseTime: '',
  domainName: '',
  ntpServers: '',
}

function splitList(value: string): string[] | undefined {
  const items = value.split(',').map((item) => item.trim()).filter(Boolean)
  return items.length > 0 ? items : undefined
}

/** IPs of machines statically assigned to this subnet — the ruler's static band. */
function staticIPsForSubnet(machines: Machine[], subnetName: string): string[] {
  return machines
    .filter((machine) => machine.subnetRef === subnetName && machine.ipAssignment === 'static' && machine.ip)
    .map((machine) => machine.ip as string)
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
  selectedSubnetData,
  dhcpLeases,
  machines,
}: NetworkViewProps) {
  const [editOpen, setEditOpen] = useState(false)
  const [editForm, setEditForm] = useState<EditForm>(EMPTY_EDIT_FORM)
  const [leaseTimePreset, setLeaseTimePreset] = useState(0)
  const [pxeRangeStart, setPxeRangeStart] = useState('')
  const [pxeRangeEnd, setPxeRangeEnd] = useState('')
  const [deleteConfirm, setDeleteConfirm] = useState<{ open: boolean; name: string }>({ open: false, name: '' })

  const addressSpace = useMemo(() => {
    if (!selectedSubnetData) return null
    return computeAddressSpace(selectedSubnetData, staticIPsForSubnet(machines, selectedSubnetData.name))
  }, [selectedSubnetData, machines])

  function openEdit() {
    if (!selectedSubnetData) return
    const { spec } = selectedSubnetData
    const leaseTime = spec.leaseTime ?? 0
    const matchedPreset = LEASE_TIME_PRESETS.find((preset) => preset.value === leaseTime)

    setEditForm({
      cidr: spec.cidr,
      defaultGateway: spec.defaultGateway ?? '',
      dnsServers: spec.dnsServers?.join(', ') ?? '',
      dnsSearchDomains: spec.dnsSearchDomains?.join(', ') ?? '',
      vlanId: spec.vlanId != null ? String(spec.vlanId) : '',
      pxeInterface: spec.pxeInterface ?? '',
      leaseTime: leaseTime > 0 ? String(leaseTime) : '',
      domainName: spec.domainName ?? '',
      ntpServers: spec.ntpServers?.join(', ') ?? '',
    })
    setLeaseTimePreset(matchedPreset ? matchedPreset.value : leaseTime > 0 ? -1 : 0)
    setPxeRangeStart(spec.pxeAddressRange?.start ?? '')
    setPxeRangeEnd(spec.pxeAddressRange?.end ?? '')
    setEditOpen(true)
  }

  function changeLeaseTimePreset(value: number) {
    setLeaseTimePreset(value)
    // A concrete preset writes its seconds through; "Default" clears the field
    // and "Custom" (-1) leaves whatever the user already typed.
    if (value > 0) setEditForm((form) => ({ ...form, leaseTime: String(value) }))
    else if (value === 0) setEditForm((form) => ({ ...form, leaseTime: '' }))
  }

  async function handleSaveEdit(event: FormEvent) {
    event.preventDefault()
    if (!selectedSubnetData) return

    const spec: Subnet['spec'] = {
      cidr: editForm.cidr,
      defaultGateway: editForm.defaultGateway || undefined,
      dnsServers: splitList(editForm.dnsServers),
      dnsSearchDomains: splitList(editForm.dnsSearchDomains),
      vlanId: editForm.vlanId ? Number(editForm.vlanId) : undefined,
      pxeInterface: editForm.pxeInterface || undefined,
      pxeAddressRange: pxeRangeStart && pxeRangeEnd ? { start: pxeRangeStart, end: pxeRangeEnd } : undefined,
      reservedRanges: selectedSubnetData.spec.reservedRanges,
      leaseTime: editForm.leaseTime ? Number(editForm.leaseTime) : undefined,
      domainName: editForm.domainName || undefined,
      ntpServers: splitList(editForm.ntpServers),
    }

    await onUpdateSubnet(selectedSubnetData.name, spec)
    setEditOpen(false)
  }

  return (
    <>
      {subnetFormOpen && (
        <CreateSubnetDialog
          subnetForm={subnetForm}
          onSubnetFormChange={onSubnetFormChange}
          onCreateSubnet={onCreateSubnet}
          onClose={onToggleSubnetForm}
        />
      )}

      {editOpen && selectedSubnetData && (
        <EditSubnetDialog
          subnet={selectedSubnetData}
          editForm={editForm}
          onEditFormChange={(field, value) => setEditForm((form) => ({ ...form, [field]: value }))}
          leaseTimePreset={leaseTimePreset}
          onLeaseTimePresetChange={changeLeaseTimePreset}
          pxeRangeStart={pxeRangeStart}
          onPxeRangeStartChange={setPxeRangeStart}
          pxeRangeEnd={pxeRangeEnd}
          onPxeRangeEndChange={setPxeRangeEnd}
          onSave={handleSaveEdit}
          onClose={() => setEditOpen(false)}
        />
      )}

      {deleteConfirm.open && (
        <DeleteSubnetDialog
          name={deleteConfirm.name}
          onConfirm={() => {
            void onDeleteSubnet(deleteConfirm.name)
            setDeleteConfirm({ open: false, name: '' })
          }}
          onClose={() => setDeleteConfirm({ open: false, name: '' })}
        />
      )}

      <div className="grid gap-[18px]">
        <SubnetTable
          subnets={subnets}
          selectedSubnet={selectedSubnet}
          onSelectSubnet={onSelectSubnet}
          onCreateSubnet={onToggleSubnetForm}
        />

        {addressSpace && <AddressSpaceRuler space={addressSpace} />}

        <div className="grid grid-cols-[300px_minmax(0,1fr)] gap-[18px] items-start max-md:grid-cols-1">
          {selectedSubnetData ? (
            <SubnetFacts
              subnet={selectedSubnetData}
              onEdit={openEdit}
              onDelete={() => setDeleteConfirm({ open: true, name: selectedSubnetData.name })}
            />
          ) : (
            <p className="m-0 font-mono text-[11.5px] text-ink-soft">Select a subnet to view details</p>
          )}

          <DHCPLeaseTable dhcpLeases={dhcpLeases} />
        </div>
      </div>
    </>
  )
}
