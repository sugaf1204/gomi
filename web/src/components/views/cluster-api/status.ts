import type { ClusterAPIResource } from './types'

type Tone = 'ok' | 'warn' | 'error' | 'neutral'
export type ResourceHealth = { label: string; tone: Tone; reason?: string; message?: string }

export function resourceHealth(resource: ClusterAPIResource): ResourceHealth {
  if (resource.deleting) return { label: 'Deleting', tone: 'warn' }
  if (resource.paused) return { label: 'Paused', tone: 'neutral' }
  const ready = resource.conditions.find((condition) => condition.type === 'Available')
    ?? resource.conditions.find((condition) => condition.type === 'Ready')
  const outdated = ready?.observedGeneration !== undefined && resource.generation !== undefined
    && ready.observedGeneration < resource.generation
  if (outdated) return { label: 'Reconciling', tone: 'warn', message: 'Waiting for the controller to observe the latest change.' }
  if (resource.phase === 'Failed' || resource.phase === 'Error') {
    return { label: resource.phase, tone: 'error', reason: ready?.reason, message: ready?.message }
  }
  if (ready?.status === 'True') return { label: 'Ready', tone: 'ok' }
  if (ready?.status === 'False') return { label: 'Not ready', tone: 'warn', reason: ready.reason, message: ready.message }
  return { label: 'Unknown', tone: 'neutral', reason: ready?.reason, message: ready?.message }
}

export const healthToneClass: Record<Tone, string> = {
  ok: 'bg-ok-bg text-ok',
  warn: 'bg-warn-bg text-warn',
  error: 'bg-error-bg text-error',
  neutral: 'bg-neutral-bg text-ink-soft',
}
