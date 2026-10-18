import { healthToneClass, resourceHealth } from './status'
import type { ClusterAPIResource } from './types'

export function ResourceStatus({ resource }: { resource: ClusterAPIResource }) {
  const health = resourceHealth(resource)
  return <span className={`inline-flex items-center w-fit shrink-0 font-mono font-semibold text-[10.5px] lowercase px-[6px] py-[4px] ${healthToneClass[health.tone]}`}>
    {health.label}
  </span>
}

/** Surface the actionable reason without requiring every raw condition to be open. */
export function ResourceIssue({ resource }: { resource: ClusterAPIResource }) {
  const health = resourceHealth(resource)
  if (!health.message && !health.reason) return null
  return <div className={`border-l-2 pl-3 py-1 text-[12px] leading-relaxed ${health.tone === 'error' ? 'border-error-line text-error' : health.tone === 'warn' ? 'border-warn-line text-ink' : 'border-line text-ink-mid'}`}>
    {health.reason && <p className="m-0 font-mono font-medium break-anywhere">{health.reason}</p>}
    {health.message && <p className="m-0 mt-1 whitespace-pre-wrap break-anywhere">{health.message}</p>}
  </div>
}

export function ResourceConditions({ resource }: { resource: ClusterAPIResource }) {
  return <div className="grid divide-y divide-line-soft">
    {resource.conditions.length === 0 && <p className="text-[12px] text-ink-soft">No conditions reported by the controller.</p>}
    {resource.conditions.map((condition) => {
      const outdated = condition.observedGeneration !== undefined && resource.generation !== undefined
        && condition.observedGeneration < resource.generation
      return <div key={condition.type} className="py-3 grid gap-1">
        <div className="flex items-baseline gap-3 flex-wrap">
          <span className="font-mono text-[12px] font-medium break-anywhere">{condition.type}</span>
          <span className="text-[11px] font-mono text-ink-mid">{condition.status}</span>
          {outdated && <span className="text-[11px] text-warn">Previous generation {condition.observedGeneration} / {resource.generation}</span>}
        </div>
        {condition.reason && <p className="m-0 font-mono text-[11px] text-ink-mid break-anywhere">{condition.reason}</p>}
        {condition.message && <p className="m-0 text-[12px] text-ink-mid leading-relaxed whitespace-pre-wrap break-anywhere">{condition.message}</p>}
      </div>
    })}
  </div>
}
