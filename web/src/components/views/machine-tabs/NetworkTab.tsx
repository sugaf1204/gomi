import { useState } from 'react'
import { api } from '../../../api'
import { notifyError } from '../../../lib/toast'
import type { Machine, Subnet } from '../../../types'

type Props = {
  machine: Machine
  subnets: Subnet[]
  onRefresh: () => void | Promise<void>
}

export function NetworkTab({ machine, subnets, onRefresh }: Props) {
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)

  const [ipAssignment, setIpAssignment] = useState(machine.ipAssignment || 'dhcp')
  const [ip, setIp] = useState(machine.ip || '')
  const [subnetRef, setSubnetRef] = useState(machine.subnetRef || '')
  const [domain, setDomain] = useState(machine.network?.domain || '')

  function startEdit() {
    setIpAssignment(machine.ipAssignment || 'dhcp')
    setIp(machine.ip || '')
    setSubnetRef(machine.subnetRef || '')
    setDomain(machine.network?.domain || '')
    setEditing(true)
  }

  function cancel() {
    setEditing(false)
  }

  async function save() {
    setSaving(true)
    try {
      await api.updateMachineNetwork(
        machine.name,
        { ip, ipAssignment, subnetRef, domain }
      )
      setEditing(false)
      await onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to save.')
    } finally {
      setSaving(false)
    }
  }

  if (!editing) {
    return (
      <div className="pt-[0.65rem] grid gap-[0.5rem]">
        <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem] max-sm:grid-cols-[110px_minmax(0,1fr)]">
          <dt className="text-ink-soft text-[0.84rem]">IP Assignment</dt>
          <dd className="m-0">{machine.ipAssignment || '-'}</dd>
          <dt className="text-ink-soft text-[0.84rem]">IP Address</dt>
          <dd className="m-0">{machine.ip || '-'}</dd>
          <dt className="text-ink-soft text-[0.84rem]">Subnet Ref</dt>
          <dd className="m-0">{machine.subnetRef || '-'}</dd>
          <dt className="text-ink-soft text-[0.84rem]">Domain</dt>
          <dd className="m-0">{machine.network?.domain || '-'}</dd>
        </dl>
        <div>
          <button
            type="button"
            onClick={startEdit}
            className="px-[0.6rem] py-[0.25rem] text-[0.84rem] bg-surface-raised text-ink border border-edge rounded cursor-pointer"
          >
            Edit
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="pt-[0.65rem] grid gap-[0.6rem] max-w-[420px]">
      <div className="grid gap-[0.35rem]">
        <label className="text-[0.84rem] text-ink-soft">IP Assignment</label>
        <div className="flex gap-[0.5rem]">
          <label className="flex items-center gap-[0.25rem] text-[0.88rem] cursor-pointer">
            <input type="radio" name="ipAssignment" value="dhcp" checked={ipAssignment === 'dhcp'} onChange={() => setIpAssignment('dhcp')} />
            DHCP
          </label>
          <label className="flex items-center gap-[0.25rem] text-[0.88rem] cursor-pointer">
            <input type="radio" name="ipAssignment" value="static" checked={ipAssignment === 'static'} onChange={() => setIpAssignment('static')} />
            Static
          </label>
        </div>
      </div>

      {ipAssignment === 'static' && (
        <div className="grid gap-[0.35rem]">
          <label className="text-[0.84rem] text-ink-soft">IP Address</label>
          <input value={ip} onChange={e => setIp(e.target.value)} placeholder="10.0.0.100" />
        </div>
      )}

      <div className="grid gap-[0.35rem]">
        <label className="text-[0.84rem] text-ink-soft">Subnet</label>
        <select value={subnetRef} onChange={e => setSubnetRef(e.target.value)}>
          <option value="">— none —</option>
          {subnets.map(s => (
            <option key={s.name} value={s.name}>{s.name} ({s.spec.cidr})</option>
          ))}
        </select>
      </div>

      <div className="grid gap-[0.35rem]">
        <label className="text-[0.84rem] text-ink-soft">Domain</label>
        <input value={domain} onChange={e => setDomain(e.target.value)} placeholder="example.local" />
      </div>

      <div className="flex gap-[0.4rem]">
        <button
          type="button"
          onClick={save}
          disabled={saving}
          className="px-[0.6rem] py-[0.25rem] text-[0.84rem] bg-brand text-white rounded cursor-pointer border-0"
        >
          {saving ? 'Saving...' : 'Save'}
        </button>
        <button
          type="button"
          onClick={cancel}
          disabled={saving}
          className="px-[0.6rem] py-[0.25rem] text-[0.84rem] bg-surface-raised text-ink border border-edge rounded cursor-pointer"
        >
          Cancel
        </button>
      </div>
    </div>
  )
}
