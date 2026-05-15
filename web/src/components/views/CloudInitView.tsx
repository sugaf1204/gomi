import { useState, useEffect } from 'react'
import clsx from 'clsx'
import { api } from '../../api'
import { formatDate } from '../../lib/formatters'
import { usePersistentStringState } from '../../hooks/usePersistentStringState'
import { notifyError } from '../../lib/toast'
import type { CloudInitTemplate } from '../../types'
import { ModalOverlay } from '../ui/ModalOverlay'

export type CloudInitViewProps = {
  cloudInits: CloudInitTemplate[]
  onRefresh: () => void | Promise<void>
}

const CLOUD_INIT_SELECTION_STORAGE_KEY = 'gomi.cloud-init.selected'

type CloudInitForm = {
  name: string
  description: string
  userData: string
  networkConfig: string
  metadataTemplate: string
}

const initialForm: CloudInitForm = {
  name: '',
  description: '',
  userData: '#cloud-config\npackage_update: true\npackages:\n  - vim\n',
  networkConfig: '',
  metadataTemplate: ''
}

export function CloudInitView({ cloudInits, onRefresh }: CloudInitViewProps) {
  const [selected, setSelected] = usePersistentStringState(CLOUD_INIT_SELECTION_STORAGE_KEY)
  const [formOpen, setFormOpen] = useState(false)
  const [form, setForm] = useState(initialForm)
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editForm, setEditForm] = useState<CloudInitForm>(initialForm)
  const [saving, setSaving] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState<{ open: boolean; name: string }>({ open: false, name: '' })

  const selectedTemplate = cloudInits.find((ci) => ci.name === selected) ?? null

  useEffect(() => {
    if (selected && cloudInits.length > 0 && !cloudInits.some((cloudInit) => cloudInit.name === selected)) {
      setSelected('')
    }
  }, [selected, cloudInits])

  useEffect(() => {
    if (!formOpen) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setFormOpen(false)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [formOpen])

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    setCreating(true)
    try {
      await api.createCloudInitTemplate({
        name: form.name,
        description: form.description || undefined,
        userData: form.userData,
        networkConfig: form.networkConfig || undefined,
        metadataTemplate: form.metadataTemplate || undefined
      })
      setForm(initialForm)
      setFormOpen(false)
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to create template')
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete(name: string) {
    try {
      await api.deleteCloudInitTemplate(name)
      if (selected === name) setSelected('')
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to delete template')
    }
  }

  function startEditing() {
    if (!selectedTemplate) return
    setEditForm({
      name: selectedTemplate.name,
      description: selectedTemplate.description || '',
      userData: selectedTemplate.userData,
      networkConfig: selectedTemplate.networkConfig || '',
      metadataTemplate: selectedTemplate.metadataTemplate || ''
    })
    setEditing(true)
  }

  async function handleSaveEdit(e: React.FormEvent) {
    e.preventDefault()
    if (!selectedTemplate) return
    setSaving(true)
    try {
      await api.updateCloudInitTemplate(selectedTemplate.name, {
        description: editForm.description || undefined,
        userData: editForm.userData,
        networkConfig: editForm.networkConfig || undefined,
        metadataTemplate: editForm.metadataTemplate || undefined
      })
      setEditing(false)
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to update template')
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      {formOpen && (
        <ModalOverlay onBackdropClick={() => { setFormOpen(false) }}>
          <div className="w-[min(480px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[80vh] overflow-y-auto">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">Create Cloud-Init Template</h3>
              <button
                aria-label="Close"
                className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                onClick={() => { setFormOpen(false) }}
              >×</button>
            </div>
            <form className="grid gap-[0.55rem]" onSubmit={(e) => void handleCreate(e)}>
              <label className="text-[0.84rem]">
                Name
                <input required value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} placeholder="e.g. ubuntu-base" />
              </label>
              <label className="text-[0.84rem]">
                Description
                <input value={form.description} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} placeholder="Optional description" />
              </label>
              <label className="text-[0.84rem]">
                User Data (YAML)
                <textarea
                  required
                  rows={6}
                  className="border border-line bg-white p-[0.55rem] font-mono text-[0.82rem] resize-y"
                  value={form.userData}
                  onChange={(e) => setForm((f) => ({ ...f, userData: e.target.value }))}
                />
              </label>
              <label className="text-[0.84rem]">
                Network Config (optional)
                <textarea
                  rows={3}
                  className="border border-line bg-white p-[0.55rem] font-mono text-[0.82rem] resize-y"
                  value={form.networkConfig}
                  onChange={(e) => setForm((f) => ({ ...f, networkConfig: e.target.value }))}
                  placeholder="Optional network config YAML"
                />
              </label>
              <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
                <button type="button" onClick={() => { setFormOpen(false) }}>Cancel</button>
                <button type="submit" disabled={creating} className="bg-brand border-brand-strong text-white">
                  {creating ? 'Creating...' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </ModalOverlay>
      )}

      <section className="min-h-0 grid grid-cols-1 md:grid-cols-[280px_minmax(0,1fr)] gap-[0.9rem]">
        <div className="min-h-0 grid grid-rows-[auto_minmax(0,1fr)] gap-[0.6rem]">
          <div className="grid gap-[0.45rem]">
            <div className="flex justify-between items-center gap-2">
              <h2 className="text-[1.4rem]">Cloud-Init</h2>
              <button
                className="bg-brand border-brand-strong text-white py-[0.35rem] px-[0.55rem] text-[0.82rem]"
                onClick={() => setFormOpen(true)}
              >
                Create
              </button>
            </div>
          </div>

          <div className="overflow-auto border-t border-line pr-[0.1rem]">
            {cloudInits.map((ci) => (
              <button
                key={ci.name}
                className={clsx(
                  'text-left w-full border-0 border-b border-line border-l-[3px] border-l-transparent bg-transparent flex justify-between items-start gap-[0.75rem] py-[0.62rem] pl-[0.55rem] pr-[0.1rem] shadow-none hover:transform-none!',
                  selected === ci.name && '!border-l-brand bg-[rgba(43,122,120,0.06)] shadow-none!'
                )}
                onClick={() => { setSelected(ci.name); setEditing(false) }}
              >
                <div>
                  <p className="m-0 font-ui font-medium tracking-normal">{ci.name}</p>
                  {ci.description && <p className="m-0 text-ink-soft text-[0.82rem]">{ci.description}</p>}
                </div>
              </button>
            ))}
            {cloudInits.length === 0 && <p className="m-0 text-ink-soft">No templates found</p>}
          </div>
        </div>

        <div className="min-h-0 grid content-start gap-[0.85rem]">
          {!selectedTemplate && (
            <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.45rem]">
              <h2>Select a template</h2>
              <p>Choose a cloud-init template from the list to view or edit its contents.</p>
            </section>
          )}

          {selectedTemplate && !editing && (
            <>
              <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] flex justify-between items-start gap-4">
                <div>
                  <p className="m-0 font-ui font-medium text-[0.72rem] uppercase tracking-[0.08em] text-ink-soft">Cloud-Init Template</p>
                  <h2 className="mt-[0.22rem] text-[1.8rem]">{selectedTemplate.name}</h2>
                  {selectedTemplate.description && <p className="m-0 text-ink-soft">{selectedTemplate.description}</p>}
                </div>
                <div className="flex flex-wrap gap-[0.35rem]">
                  <button className="bg-brand border-brand-strong text-white py-[0.45rem] px-[0.72rem]" onClick={startEditing}>
                    Edit
                  </button>
                  <button
                    className="bg-[#d86b6b] border-[#be5252] text-white py-[0.45rem] px-[0.72rem]"
                    onClick={() => setDeleteConfirm({ open: true, name: selectedTemplate.name })}
                  >
                    Delete
                  </button>
                </div>
              </section>

              <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                <h3 className="mb-[0.58rem] text-[1.05rem]">User Data</h3>
                <pre className="m-0 p-[0.65rem] border border-line bg-panel-2 font-mono text-[0.82rem] overflow-auto max-h-[40vh] whitespace-pre-wrap">{selectedTemplate.userData}</pre>
              </section>

              {selectedTemplate.networkConfig && (
                <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                  <h3 className="mb-[0.58rem] text-[1.05rem]">Network Config</h3>
                  <pre className="m-0 p-[0.65rem] border border-line bg-panel-2 font-mono text-[0.82rem] overflow-auto max-h-[30vh] whitespace-pre-wrap">{selectedTemplate.networkConfig}</pre>
                </section>
              )}

              {selectedTemplate.metadataTemplate && (
                <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                  <h3 className="mb-[0.58rem] text-[1.05rem]">Metadata Template</h3>
                  <pre className="m-0 p-[0.65rem] border border-line bg-panel-2 font-mono text-[0.82rem] overflow-auto max-h-[30vh] whitespace-pre-wrap">{selectedTemplate.metadataTemplate}</pre>
                </section>
              )}

              <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                  <dt className="text-ink-soft text-[0.84rem]">Created</dt><dd className="m-0">{formatDate(selectedTemplate.createdAt)}</dd>
                  <dt className="text-ink-soft text-[0.84rem]">Updated</dt><dd className="m-0">{formatDate(selectedTemplate.updatedAt)}</dd>
                </dl>
              </section>
            </>
          )}

          {selectedTemplate && editing && (
            <form className="grid gap-[0.65rem] pt-[0.85rem] border-t border-line" onSubmit={(e) => void handleSaveEdit(e)}>
              <div className="flex justify-between items-center">
                <h2 className="text-[1.4rem]">Edit: {selectedTemplate.name}</h2>
                <div className="flex gap-[0.35rem]">
                  <button type="button" className="py-[0.45rem] px-[0.72rem]" onClick={() => setEditing(false)}>Cancel</button>
                  <button type="submit" disabled={saving} className="bg-brand border-brand-strong text-white py-[0.45rem] px-[0.72rem]">
                    {saving ? 'Saving...' : 'Save'}
                  </button>
                </div>
              </div>
              <label className="text-[0.84rem]">
                Description
                <input value={editForm.description} onChange={(e) => setEditForm((f) => ({ ...f, description: e.target.value }))} />
              </label>
              <label className="text-[0.84rem]">
                User Data (YAML)
                <textarea
                  required
                  rows={12}
                  className="border border-line bg-white p-[0.55rem] font-mono text-[0.82rem] resize-y"
                  value={editForm.userData}
                  onChange={(e) => setEditForm((f) => ({ ...f, userData: e.target.value }))}
                />
              </label>
              <label className="text-[0.84rem]">
                Network Config (optional)
                <textarea
                  rows={6}
                  className="border border-line bg-white p-[0.55rem] font-mono text-[0.82rem] resize-y"
                  value={editForm.networkConfig}
                  onChange={(e) => setEditForm((f) => ({ ...f, networkConfig: e.target.value }))}
                />
              </label>
              <label className="text-[0.84rem]">
                Metadata Template (optional)
                <textarea
                  rows={4}
                  className="border border-line bg-white p-[0.55rem] font-mono text-[0.82rem] resize-y"
                  value={editForm.metadataTemplate}
                  onChange={(e) => setEditForm((f) => ({ ...f, metadataTemplate: e.target.value }))}
                />
              </label>
            </form>
          )}
        </div>
      </section>

      {deleteConfirm.open && (
        <ModalOverlay onBackdropClick={() => setDeleteConfirm({ open: false, name: '' })}>
          <div className="w-[min(400px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[0.95rem] grid gap-[0.6rem]">
            <h3 className="text-[1.2rem] text-[#9b2d2d]">Delete Cloud-Init Template</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">Are you sure you want to delete this template?</p>
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
        </ModalOverlay>
      )}
    </>
  )
}
