import type { ReactNode } from 'react'
import { ResourceIssue, ResourceStatus } from './ResourceStatus'
import type { ClusterAPIResource } from './types'

export function GroupSummary({ resource, children }: { resource: ClusterAPIResource; children?: ReactNode }) {
  const incomplete = resource.readyReplicas !== undefined && resource.replicas !== undefined && resource.readyReplicas < resource.replicas
  return <article className="grid gap-3 py-4 border-b border-line-soft min-w-0">
    <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-4 items-start">
      <div className="min-w-0 grid gap-1.5">
        <h4 className="m-0 font-mono font-medium text-[12.5px] break-anywhere">{resource.name}</h4>
        <div className="flex items-center gap-2 flex-wrap">
          <ResourceStatus resource={resource} />
          <span className="text-[11px] font-mono text-ink-mid">{resource.version || 'Version unknown'}</span>
        </div>
      </div>
      <div className="text-right">
        <p className="m-0 font-mono text-[20px] leading-tight"><span className={incomplete ? 'text-warn' : 'text-ink'}>{resource.readyReplicas ?? '—'}</span><span className="text-ink-soft text-[14px]"> / {resource.replicas ?? '—'}</span></p>
        <p className="m-0 mt-1 text-[10px] text-ink-soft uppercase tracking-[0.08em]">Ready / desired</p>
      </div>
    </div>
    <ResourceIssue resource={resource} />
    {children}
  </article>
}
