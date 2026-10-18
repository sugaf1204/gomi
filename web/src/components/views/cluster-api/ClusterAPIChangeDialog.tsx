import { useState } from 'react'
import { api } from '../../../api'
import { ModalOverlay } from '../../ui/ModalOverlay'
import type { ClusterAPIResource } from './types'

export type PendingChange = { resource: ClusterAPIResource; replicas?: number; paused?: boolean }

type Props = { pending: PendingChange; context?: string; unavailable?: boolean; onClose: () => void; onApplied: () => void }

export function ClusterAPIChangeDialog({ pending, context, unavailable, onClose, onApplied }: Props) {
  const [confirmation, setConfirmation] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const { resource, replicas, paused } = pending
  const scaling = replicas !== undefined
  const title = scaling ? 'Change worker count' : paused ? 'Pause cluster reconciliation' : 'Resume cluster reconciliation'
  const target = `${resource.namespace}/${resource.name}`

  async function apply() {
    setSaving(true)
    setError('')
    try {
      await api.changeClusterAPI(resource, {
        apiVersion: resource.apiVersion, uid: resource.uid, resourceVersion: resource.resourceVersion,
        ...(scaling ? { replicas } : { paused }),
      })
      onApplied()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update desired state')
    } finally { setSaving(false) }
  }

  return <ModalOverlay onBackdropClick={() => { if (!saving) onClose() }}>
    <form className="w-[min(500px,100%)] bg-panel border border-line-strong shadow-xl p-5 grid gap-4 max-h-[90vh] overflow-y-auto" aria-label={title} onKeyDown={(event) => { if (event.key === 'Escape' && !saving) onClose() }} onSubmit={(event) => { event.preventDefault(); if (confirmation === target && !saving && !unavailable) void apply() }}>
      <h3 className="m-0 pb-3 border-b border-line text-[18px]">{title}</h3>
      <div className="bg-panel-2 border border-line p-3 text-[12px] grid gap-2">
        <div>Management context: <strong>{context || 'Configured context'}</strong></div>
        <code className="break-all">{target}</code>
        {scaling && <div>Desired workers: <strong>{resource.replicas ?? 'unset'} → {replicas}</strong></div>}
      </div>
      <p className="m-0 text-[12px] text-ink-soft">{scaling
        ? replicas < (resource.replicas ?? 0)
          ? 'Scaling down drains and deletes worker nodes and their infrastructure. Workloads may be disrupted. CAPI chooses the nodes to remove.'
          : 'Scaling up provisions additional nodes and consumes infrastructure capacity.'
        : paused
          ? 'Controllers stop reconciling this cluster. Existing workloads keep running, but repairs and pending changes may wait until reconciliation resumes.'
          : 'Controllers resume reconciliation, including pending provisioning, replacement and deletion operations.'}</p>
      <label className="grid min-w-0 gap-1 text-[12px] break-anywhere">Type {target} to confirm
        <input className="min-w-0 w-full" aria-label="Confirm resource name" autoFocus value={confirmation} onChange={(event) => setConfirmation(event.target.value)} disabled={saving} autoComplete="off" />
      </label>
      {unavailable && <p role="alert" className="m-0 text-danger text-[12px]">Operations are unavailable. Cancel and refresh before applying.</p>}
      {error && <p role="alert" className="m-0 text-danger text-[12px]">{error}</p>}
      <div className="flex justify-end gap-2 pt-3 border-t border-line">
        <button className="py-[7px] px-[11px] text-[12px]" type="button" onClick={onClose} disabled={saving}>Cancel</button>
        <button className="bg-brand border-brand-strong text-white py-[7px] px-[11px] text-[12px] font-medium" type="submit" disabled={saving || unavailable || confirmation !== target}>{saving ? 'Applying…' : 'Apply change'}</button>
      </div>
    </form>
  </ModalOverlay>
}
