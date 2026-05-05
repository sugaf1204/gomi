import { useState } from 'react'
import { api } from '../../api'
import { notifyError } from '../../lib/toast'
import type { SSHKey } from '../../types'

type SSHAccessValue = {
  sshKeyRefs: string[]
  loginUserUsername: string
  loginUserPassword: string
}

type SSHKeyForm = {
  name: string
  publicKey: string
  privateKey: string
  comment: string
}

type SSHAccessFieldsetProps = {
  sshKeys: SSHKey[]
  value: SSHAccessValue
  onChange: (updater: (current: SSHAccessValue) => SSHAccessValue) => void
  onRefresh: () => void | Promise<void>
}

const initialKeyForm: SSHKeyForm = { name: '', publicKey: '', privateKey: '', comment: '' }

export function SSHAccessFieldset({ sshKeys, value, onChange, onRefresh }: SSHAccessFieldsetProps) {
  const [addOpen, setAddOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [keyForm, setKeyForm] = useState(initialKeyForm)

  async function handleCreateKey() {
    const name = keyForm.name.trim()
    const publicKey = keyForm.publicKey.trim()
    if (!name || !publicKey) return

    setCreating(true)
    try {
      const created = await api.createSSHKey({
        name,
        publicKey,
        privateKey: keyForm.privateKey.trim() || undefined,
        comment: keyForm.comment.trim() || undefined,
      })
      const createdName = created.name || name
      onChange((current) => ({
        ...current,
        sshKeyRefs: current.sshKeyRefs.includes(createdName)
          ? current.sshKeyRefs
          : [...current.sshKeyRefs, createdName],
      }))
      setKeyForm(initialKeyForm)
      setAddOpen(false)
      await Promise.resolve(onRefresh()).catch(() => undefined)
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to create SSH key')
    } finally {
      setCreating(false)
    }
  }

  return (
    <fieldset className="border border-line rounded p-0 m-0">
      <legend className="text-[0.84rem] font-medium px-[0.4rem] ml-[0.3rem]">SSH Access</legend>
      <div className="grid gap-[0.45rem] p-[0.7rem]">
        <div className="flex items-start justify-between gap-[0.6rem]">
          <p className="m-0 text-[0.76rem] text-ink-soft">
            Selected keys are installed on the OS distribution's default user (e.g. <code>ubuntu</code>) and
            on the optional extra login user below. Password SSH login is disabled.
          </p>
          <button
            type="button"
            className="py-[0.35rem] px-[0.55rem] text-[0.82rem] shrink-0"
            onClick={() => setAddOpen((current) => !current)}
            disabled={creating}
          >
            {addOpen ? 'Cancel' : 'Add Key'}
          </button>
        </div>

        {addOpen && (
          <div
            className="grid gap-[0.45rem] border border-line bg-[#f9f7f4] p-[0.55rem]"
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !(e.target instanceof HTMLTextAreaElement)) {
                e.preventDefault()
                void handleCreateKey()
              }
            }}
          >
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
              <label className="text-[0.84rem]">
                Key Name
                <input
                  placeholder="e.g. alice-laptop"
                  value={keyForm.name}
                  onChange={(e) => setKeyForm((current) => ({ ...current, name: e.target.value }))}
                />
              </label>
              <label className="text-[0.84rem]">
                Comment (optional)
                <input
                  placeholder="e.g. development machine"
                  value={keyForm.comment}
                  onChange={(e) => setKeyForm((current) => ({ ...current, comment: e.target.value }))}
                />
              </label>
            </div>
            <label className="text-[0.84rem]">
              Public Key
              <textarea
                rows={3}
                className="border border-line bg-white p-[0.55rem] font-mono text-[0.84rem] resize-y"
                placeholder="ssh-ed25519 AAAA..."
                value={keyForm.publicKey}
                onChange={(e) => setKeyForm((current) => ({ ...current, publicKey: e.target.value }))}
              />
            </label>
            <label className="text-[0.84rem]">
              Private Key - PEM (optional)
              <textarea
                rows={3}
                className="border border-line bg-white p-[0.55rem] font-mono text-[0.84rem] resize-y"
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                value={keyForm.privateKey}
                onChange={(e) => setKeyForm((current) => ({ ...current, privateKey: e.target.value }))}
              />
            </label>
            <div className="flex justify-end">
              <button
                type="button"
                className="bg-brand border-brand-strong text-white"
                onClick={() => void handleCreateKey()}
                disabled={creating || !keyForm.name.trim() || !keyForm.publicKey.trim()}
              >
                {creating ? 'Adding...' : 'Save Key'}
              </button>
            </div>
          </div>
        )}

        <div className="grid gap-[0.25rem] max-h-[160px] overflow-y-auto border border-line bg-white p-[0.4rem]">
          {sshKeys.length === 0 && (
            <span className="text-[0.78rem] text-ink-soft">No SSH keys registered. Add one here or in the Users view.</span>
          )}
          {sshKeys.map((k) => {
            const checked = value.sshKeyRefs.includes(k.name)
            return (
              <label key={k.name} className="flex items-start gap-[0.4rem] text-[0.82rem] cursor-pointer">
                <input
                  type="checkbox"
                  checked={checked}
                  onChange={(e) => onChange((current) => ({
                    ...current,
                    sshKeyRefs: e.target.checked
                      ? [...current.sshKeyRefs, k.name]
                      : current.sshKeyRefs.filter((n) => n !== k.name)
                  }))}
                />
                <span className="min-w-0">
                  <span className="font-medium">{k.name}</span>
                  {k.comment && <span className="text-ink-soft"> - {k.comment}</span>}
                </span>
              </label>
            )
          })}
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.4rem]">
          <label className="text-[0.84rem]">
            Extra login user (optional)
            <input
              placeholder="e.g. admin"
              value={value.loginUserUsername}
              onChange={(e) => onChange((current) => ({ ...current, loginUserUsername: e.target.value }))}
            />
          </label>
          <label className="text-[0.84rem]">
            Password (optional, console only)
            <input
              type="password"
              placeholder="leave blank for key-only"
              value={value.loginUserPassword}
              onChange={(e) => onChange((current) => ({ ...current, loginUserPassword: e.target.value }))}
              disabled={!value.loginUserUsername.trim()}
            />
          </label>
        </div>
      </div>
    </fieldset>
  )
}
