import { useState, type FormEvent } from 'react'
import type { PendingChange } from './ClusterAPIChangeDialog'
import { GroupSummary } from './GroupSummary'
import { scaleRestriction, type ClusterAPIResource } from './types'

type Props = { cluster: ClusterAPIResource; group: ClusterAPIResource; canWrite: boolean; onChange: (change: PendingChange) => void }

export function WorkerGroup({ cluster, group, canWrite, onChange }: Props) {
  const [replicas, setReplicas] = useState(String(group.replicas ?? ''))
  const restriction = scaleRestriction(cluster, group)
  const valid = /^\d+$/.test(replicas) && Number(replicas) <= 2147483647
  const changed = Number(replicas) !== group.replicas

  function review(event: FormEvent) {
    event.preventDefault()
    if (valid && !restriction && changed) onChange({ resource: group, replicas: Number(replicas) })
  }

  return <GroupSummary resource={group}>
    {canWrite && <form className="flex items-end gap-2 flex-wrap" onSubmit={review}>
      <label className="text-[11px] text-ink-mid">Desired workers
        <input className="w-24 py-[6px] px-2 font-mono text-[12px]" aria-label={`Desired workers for ${group.name}`}
          type="number" min="0" max="2147483647" step="1" value={replicas}
          onChange={(event) => setReplicas(event.target.value)} disabled={Boolean(restriction)} />
      </label>
      <button className="bg-brand border-brand-strong text-white py-[7px] px-[11px] text-[12px] font-medium"
        type="submit" disabled={Boolean(restriction) || !valid || !changed}>Review change</button>
    </form>}
    {restriction && <p className="m-0 text-[11.5px] text-ink-mid">{restriction}</p>}
  </GroupSummary>
}
