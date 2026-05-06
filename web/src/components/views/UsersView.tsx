import { useState } from 'react'
import { api } from '../../api'
import { formatDate } from '../../lib/formatters'
import { notifyError } from '../../lib/toast'
import type { SSHKey } from '../../types'

export type UsersViewProps = {
  sshKeys: SSHKey[]
  onRefresh: () => void | Promise<void>
}

type SSHKeyForm = {
  name: string
  publicKey: string
  privateKey: string
  comment: string
}

const initialForm: SSHKeyForm = { name: '', publicKey: '', privateKey: '', comment: '' }

export function UsersView({ sshKeys, onRefresh }: UsersViewProps) {
  const [form, setForm] = useState(initialForm)
  const [creating, setCreating] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState<{ open: boolean; name: string }>({ open: false, name: '' })

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    setCreating(true)
    try {
      await api.createSSHKey({
        name: form.name,
        publicKey: form.publicKey,
        privateKey: form.privateKey || undefined,
        comment: form.comment || undefined,
      })
      setForm(initialForm)
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to create SSH key')
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete(name: string) {
    try {
      await api.deleteSSHKey(name)
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to delete SSH key')
    }
  }

  return (
    <section className="grid gap-[0.9rem]">
      <h2 className="text-[1.4rem]">SSH Keys</h2>
      <p className="m-0 text-ink-soft text-[0.84rem]">
        Registered keys are available to attach to a Machine or VM at deploy time. They are
        installed for the OS distribution's default user (e.g. <code>ubuntu</code>, <code>debian</code>)
        unless an SSH login user is configured on the target.
      </p>

      <form onSubmit={(e) => void handleCreate(e)} className="grid gap-[0.5rem] border border-line bg-panel p-[0.75rem]">
        <h3 className="text-[1.05rem]">Add SSH Key</h3>
        <label>
          Key Name
          <input
            required
            placeholder="e.g. alice-laptop"
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
          />
        </label>
        <label>
          Public Key
          <textarea
            required
            rows={3}
            className="border border-line bg-white p-[0.55rem] font-mono text-[0.84rem] resize-y"
            placeholder="ssh-ed25519 AAAA..."
            value={form.publicKey}
            onChange={(e) => setForm((f) => ({ ...f, publicKey: e.target.value }))}
          />
        </label>
        <label>
          Private Key - PEM (optional)
          <textarea
            rows={4}
            className="border border-line bg-white p-[0.55rem] font-mono text-[0.84rem] resize-y"
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;..."
            value={form.privateKey}
            onChange={(e) => setForm((f) => ({ ...f, privateKey: e.target.value }))}
          />
          <span className="text-ink-soft text-[0.76rem]">Required for machine SSH connections. Admin use only.</span>
        </label>
        <label>
          Comment (optional)
          <input
            placeholder="e.g. development machine"
            value={form.comment}
            onChange={(e) => setForm((f) => ({ ...f, comment: e.target.value }))}
          />
        </label>
        <div>
          <button type="submit" disabled={creating} className="bg-brand border-brand-strong text-white">
            {creating ? 'Adding...' : 'Add Key'}
          </button>
        </div>
      </form>

      {sshKeys.length === 0 && (
        <p className="m-0 text-ink-soft">No SSH keys registered</p>
      )}

      {sshKeys.length > 0 && (
        <div className="grid gap-[0.35rem]">
          {sshKeys.map((k) => (
            <article key={k.name} className="flex justify-between items-start gap-[0.6rem] border border-line bg-panel p-[0.55rem]">
              <div className="min-w-0">
                <p className="m-0 font-medium">{k.name}</p>
                <p className="m-0 text-ink-soft text-[0.82rem]">
                  {k.keyType || 'unknown'} {k.comment && `- ${k.comment}`}
                </p>
                <code className="block mt-[0.25rem] text-[0.76rem] font-mono text-ink-soft truncate max-w-full">
                  {k.publicKey.substring(0, 80)}...
                </code>
                {k.createdAt && (
                  <p className="m-0 text-ink-soft text-[0.76rem] mt-[0.15rem]">Added {formatDate(k.createdAt)}</p>
                )}
              </div>
              <button
                className="bg-[#d86b6b] border-[#be5252] text-white py-[0.35rem] px-[0.55rem] text-[0.82rem] shrink-0"
                onClick={() => setDeleteConfirm({ open: true, name: k.name })}
              >
                Delete
              </button>
            </article>
          ))}
        </div>
      )}
      {deleteConfirm.open && (
        <div
          className="fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4"
          role="dialog"
          aria-modal="true"
          onClick={(e) => { if (e.target === e.currentTarget) setDeleteConfirm({ open: false, name: '' }) }}
        >
          <div className="w-[min(400px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[0.95rem] grid gap-[0.6rem]">
            <h3 className="text-[1.2rem] text-[#9b2d2d]">Delete SSH Key</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">Are you sure you want to delete this SSH key?</p>
            <div className="border border-line bg-[#f9f7f4] p-[0.55rem]">
              <code>{deleteConfirm.name}</code>
            </div>
            <div className="flex justify-end gap-[0.45rem]">
              <button onClick={() => setDeleteConfirm({ open: false, name: '' })}>Cancel</button>
              <button
                className="bg-[#d86b6b] border-[#be5252] text-white"
                onClick={() => { void handleDelete(deleteConfirm.name); setDeleteConfirm({ open: false, name: '' }) }}
              >
                Delete
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
