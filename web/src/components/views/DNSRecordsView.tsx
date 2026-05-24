import { useState, type FormEvent } from 'react'
import { api } from '../../api'
import { formatDate } from '../../lib/formatters'
import { notifyError } from '../../lib/toast'
import type { DNSRecord } from '../../types'
import { ModalOverlay } from '../ui/ModalOverlay'

export type DNSRecordsViewProps = {
  dnsRecords: DNSRecord[]
  dnsRecordsError: string
  onRefresh: () => void | Promise<void>
}

type DNSRecordForm = {
  name: string
  type: DNSRecord['type']
  ttl: string
  values: string
}

const initialForm: DNSRecordForm = { name: '', type: 'A', ttl: '', values: '' }
const recordTypes: DNSRecord['type'][] = ['A', 'CNAME', 'TXT']

function splitValues(raw: string) {
  return raw
    .split(/[\n,]/)
    .map((value) => value.trim())
    .filter(Boolean)
}

function recordKey(record: DNSRecord) {
  return `${record.name}:${record.type}`
}

export function DNSRecordsView({ dnsRecords, dnsRecordsError, onRefresh }: DNSRecordsViewProps) {
  const [form, setForm] = useState<DNSRecordForm>(initialForm)
  const [editing, setEditing] = useState<DNSRecord | null>(null)
  const [saving, setSaving] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState<{ open: boolean; record: DNSRecord | null }>({ open: false, record: null })

  const unavailable = Boolean(dnsRecordsError)

  function updateForm(field: keyof DNSRecordForm, value: string) {
    setForm((current) => ({ ...current, [field]: value }))
  }

  function openEdit(record: DNSRecord) {
    setEditing(record)
    setForm({
      name: record.name,
      type: record.type,
      ttl: String(record.ttl),
      values: record.values.join('\n')
    })
  }

  function resetForm() {
    setEditing(null)
    setForm(initialForm)
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const payload = {
      name: form.name.trim(),
      type: form.type,
      ttl: form.ttl.trim() ? Number(form.ttl) : 0,
      values: splitValues(form.values)
    }
    setSaving(true)
    try {
      if (editing) {
        await api.updateDNSRecord(payload)
      } else {
        await api.createDNSRecord(payload)
      }
      resetForm()
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to save DNS record')
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(record: DNSRecord) {
    try {
      await api.deleteDNSRecord(record.name, record.type)
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to delete DNS record')
    }
  }

  return (
    <section className="grid gap-[0.9rem]">
      <div className="flex justify-between items-start gap-4 max-sm:flex-col">
        <div>
          <h2 className="text-[1.4rem]">DNS Records</h2>
          <p className="m-0 text-ink-soft text-[0.84rem]">Manual records stored by embedded DNS.</p>
        </div>
        {editing && <button onClick={resetForm}>New Record</button>}
      </div>

      {unavailable && (
        <div className="border border-[#d8b25f] bg-[#fff8e6] text-[#6f4f12] p-[0.65rem] text-[0.84rem]">
          {dnsRecordsError}
        </div>
      )}

      <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-[0.55rem] border border-line bg-panel p-[0.75rem]">
        <h3 className="text-[1.05rem]">{editing ? 'Edit DNS Record' : 'Add DNS Record'}</h3>
        <div className="grid grid-cols-[minmax(0,1fr)_130px_110px] gap-[0.5rem] max-md:grid-cols-1">
          <label>
            Name
            <input
              required
              disabled={Boolean(editing) || unavailable}
              value={form.name}
              onChange={(e) => updateForm('name', e.target.value)}
              placeholder="e.g. app.lab.local"
            />
          </label>
          <label>
            Type
            <div className="flex gap-[0.25rem] pt-[0.18rem]">
              {recordTypes.map((type) => (
                <button
                  key={type}
                  type="button"
                  disabled={Boolean(editing) || unavailable}
                  className={form.type === type ? 'bg-brand border-brand-strong text-white' : ''}
                  onClick={() => updateForm('type', type)}
                >
                  {type}
                </button>
              ))}
            </div>
          </label>
          <label>
            TTL
            <input
              type="number"
              min={0}
              disabled={unavailable}
              value={form.ttl}
              onChange={(e) => updateForm('ttl', e.target.value)}
              placeholder="default"
            />
          </label>
        </div>
        <label>
          Values
          <textarea
            required
            rows={form.type === 'TXT' ? 3 : 2}
            disabled={unavailable}
            className="border border-line bg-white p-[0.55rem] font-mono text-[0.84rem] resize-y"
            value={form.values}
            onChange={(e) => updateForm('values', e.target.value)}
            placeholder={form.type === 'A' ? '192.168.2.10' : form.type === 'CNAME' ? 'target.lab.local' : 'text value'}
          />
        </label>
        <div className="flex justify-end gap-[0.45rem]">
          {editing && <button type="button" onClick={resetForm}>Cancel</button>}
          <button type="submit" disabled={saving || unavailable} className="bg-brand border-brand-strong text-white">
            {saving ? 'Saving...' : editing ? 'Save' : 'Add Record'}
          </button>
        </div>
      </form>

      <div className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.65rem] self-start">
        <h3 className="text-[1.05rem]">Registered Records</h3>
        <div className="overflow-auto max-h-[58vh]">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>TTL</th>
                <th>Values</th>
                <th>Updated</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {dnsRecords.map((record) => (
                <tr key={recordKey(record)}>
                  <td><code>{record.name}</code></td>
                  <td>{record.type}</td>
                  <td>{record.ttl}</td>
                  <td>
                    <div className="grid gap-[0.15rem]">
                      {record.values.map((value) => <code key={value}>{value}</code>)}
                    </div>
                  </td>
                  <td>{formatDate(record.updatedAt)}</td>
                  <td>
                    <div className="flex gap-[0.35rem] justify-end">
                      <button className="py-[0.28rem] px-[0.55rem] text-[0.78rem]" onClick={() => openEdit(record)}>Edit</button>
                      <button
                        className="bg-[#d86b6b] border-[#be5252] text-white py-[0.28rem] px-[0.55rem] text-[0.78rem]"
                        onClick={() => setDeleteConfirm({ open: true, record })}
                      >
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {dnsRecords.length === 0 && <p className="m-0 text-ink-soft">No DNS records found</p>}
        </div>
      </div>

      {deleteConfirm.open && deleteConfirm.record && (
        <ModalOverlay onBackdropClick={() => setDeleteConfirm({ open: false, record: null })}>
          <div className="w-[min(400px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[0.95rem] grid gap-[0.6rem]">
            <h3 className="text-[1.2rem] text-[#9b2d2d]">Delete DNS Record</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">Are you sure you want to delete this DNS record?</p>
            <div className="border border-line bg-[#f9f7f4] p-[0.55rem]">
              <code>{deleteConfirm.record.name} {deleteConfirm.record.type}</code>
            </div>
            <div className="flex justify-end gap-[0.45rem]">
              <button onClick={() => setDeleteConfirm({ open: false, record: null })}>Cancel</button>
              <button
                className="bg-[#d86b6b] border-[#be5252] text-white"
                onClick={() => {
                  if (deleteConfirm.record) void handleDelete(deleteConfirm.record)
                  setDeleteConfirm({ open: false, record: null })
                }}
              >
                Delete
              </button>
            </div>
          </div>
        </ModalOverlay>
      )}
    </section>
  )
}
