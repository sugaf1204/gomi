import type { ProvisionProgress, ProvisionTiming } from '../types'
import { buildPhaseSegments, type PhaseSegment } from './deploy-phases'

export type TimeSource = 'wall' | 'monotonic' | 'unresolved'

export type ResolvedEvent = {
  index: number
  source: string
  name: string
  eventType?: string
  message?: string
  result?: string
  startMs?: number
  endMs?: number
  durationMs?: number
  monotonicSeconds?: number
  sortMs?: number
  failed: boolean
  timeSource: TimeSource
}

export type NoticeKind =
  | 'missing-start'
  | 'unresolved-events'
  | 'clock-skew'
  | 'stalled'
  | 'not-finalized'

export type DataQualityNotice = { kind: NoticeKind; text: string }

export type DeployStatus = 'complete' | 'failed' | 'in-progress' | 'empty'

export type DeployTimeline = {
  status: DeployStatus
  startMs?: number
  endMs?: number
  totalMs: number
  segments: PhaseSegment[]
  events: ResolvedEvent[]
  notices: DataQualityNotice[]
}

const STALL_THRESHOLD_MS = 120_000
const SKEW_EPSILON_MS = 1000

function parseMs(value?: string): number | undefined {
  if (!value) return undefined
  const ms = Date.parse(value)
  return Number.isFinite(ms) ? ms : undefined
}

function isFailureEvent(t: ProvisionTiming): boolean {
  const result = t.result?.toLowerCase()
  const eventType = t.eventType?.toLowerCase()
  return result === 'failure' || result === 'failed' || eventType === 'failure' || eventType === 'failed'
}

// Normalizes the three timing representations (wall-clock timestamps,
// explicit durationMs, monotonic boot offsets) into one time model. Monotonic
// offsets count from the installer kernel's boot, not from the provision
// start, so they cannot honestly be placed on the wall-clock timeline; they
// keep their boot-relative offset for display instead. Events that resolve to
// nothing keep their metadata and are flagged 'unresolved'.
function resolveEvent(t: ProvisionTiming, index: number): ResolvedEvent {
  let startMs = parseMs(t.startedAt)
  let endMs = parseMs(t.finishedAt)
  const pointMs = parseMs(t.timestamp)
  const explicitDuration =
    typeof t.durationMs === 'number' && Number.isFinite(t.durationMs) && t.durationMs > 0 ? t.durationMs : undefined
  const monotonicSeconds =
    typeof t.monotonicSeconds === 'number' && Number.isFinite(t.monotonicSeconds) ? t.monotonicSeconds : undefined
  let timeSource: TimeSource = 'wall'

  if (startMs === undefined && endMs === undefined && pointMs !== undefined) {
    startMs = pointMs
  }
  if (startMs === undefined && endMs !== undefined && explicitDuration !== undefined) {
    startMs = endMs - explicitDuration
  }
  if (endMs === undefined && startMs !== undefined && explicitDuration !== undefined) {
    endMs = startMs + explicitDuration
  }
  if (startMs === undefined && endMs === undefined) {
    timeSource = monotonicSeconds !== undefined ? 'monotonic' : 'unresolved'
  }

  const durationMs =
    explicitDuration ?? (startMs !== undefined && endMs !== undefined && endMs > startMs ? endMs - startMs : undefined)

  return {
    index,
    source: t.source?.trim() || 'unknown',
    name: t.name,
    eventType: t.eventType,
    message: t.message,
    result: t.result,
    startMs,
    endMs,
    durationMs,
    monotonicSeconds,
    sortMs: startMs ?? endMs,
    failed: isFailureEvent(t),
    timeSource,
  }
}

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? '' : 's'}`
}

export function deriveDeployTimeline(provision: ProvisionProgress | undefined, nowMs: number): DeployTimeline {
  if (!provision) {
    return { status: 'empty', totalMs: 0, segments: [], events: [], notices: [] }
  }

  const notices: DataQualityNotice[] = []
  const provStartMs = parseMs(provision.startedAt)
  const events = (provision.timings ?? []).map((t, i) => resolveEvent(t, i))

  // Unresolved events inherit their predecessor's position so time-ordering
  // keeps them next to where they were reported.
  let lastSort = provStartMs
  for (const event of events) {
    if (event.sortMs === undefined) event.sortMs = lastSort
    else lastSort = event.sortMs
  }
  const ordered = [...events].sort((a, b) => (a.sortMs ?? 0) - (b.sortMs ?? 0) || a.index - b.index)

  const unresolvedCount = events.filter((e) => e.timeSource === 'unresolved').length
  if (unresolvedCount > 0) {
    notices.push({
      kind: 'unresolved-events',
      text: `${plural(unresolvedCount, 'event')} reported no usable timestamp; listed in reported order but not drawn on the timeline.`,
    })
  }

  const completedMs = parseMs(provision.completedAt)
  const finishedMs = parseMs(provision.finishedAt)
  const lastSignalMs = parseMs(provision.lastSignalAt)
  const active = provision.active === true

  let status: DeployStatus
  if (completedMs !== undefined) {
    status = 'complete'
  } else if (finishedMs !== undefined || provision.failureReason) {
    status = 'failed'
  } else if (active) {
    status = 'in-progress'
  } else {
    status = 'failed'
    notices.push({
      kind: 'not-finalized',
      text: 'This attempt is no longer active but recorded neither completion nor failure; treating it as failed.',
    })
  }

  const resolvedTimes = events.flatMap((e) => [e.startMs, e.endMs]).filter((v): v is number => v !== undefined)

  let startMs = provStartMs
  if (startMs === undefined) {
    startMs = resolvedTimes.length > 0 ? Math.min(...resolvedTimes) : undefined
    if (startMs !== undefined) {
      notices.push({
        kind: 'missing-start',
        text: 'The attempt has no recorded start time; the timeline starts at the earliest reported event.',
      })
    }
  }
  if (startMs === undefined) {
    return { status: events.length === 0 ? 'empty' : status, totalMs: 0, segments: [], events: ordered, notices }
  }

  let endMs: number
  if (status === 'complete') {
    endMs = completedMs ?? startMs
  } else if (status === 'failed') {
    endMs =
      finishedMs ??
      lastSignalMs ??
      (resolvedTimes.length > 0 ? Math.max(...resolvedTimes) : startMs)
  } else {
    endMs = nowMs
  }
  if (endMs < startMs) {
    notices.push({
      kind: 'clock-skew',
      text: 'The attempt end precedes its start; the timeline span was clamped.',
    })
    endMs = startMs
  }

  // Events outside the attempt window are clamped so the segment invariant
  // (sum of segments == total) cannot be broken by skewed reporters.
  let clampedCount = 0
  for (const event of ordered) {
    let clamped = false
    if (event.startMs !== undefined) {
      const next = Math.min(Math.max(event.startMs, startMs), endMs)
      if (Math.abs(next - event.startMs) > SKEW_EPSILON_MS) clamped = true
      event.startMs = next
    }
    if (event.endMs !== undefined) {
      const next = Math.min(Math.max(event.endMs, startMs), endMs)
      if (Math.abs(next - event.endMs) > SKEW_EPSILON_MS) clamped = true
      event.endMs = next
    }
    if (event.sortMs !== undefined) {
      event.sortMs = Math.min(Math.max(event.sortMs, startMs), endMs)
    }
    if (clamped) clampedCount += 1
  }
  if (clampedCount > 0) {
    notices.push({
      kind: 'clock-skew',
      text: `${plural(clampedCount, 'event')} reported timestamps outside the attempt window and were clamped to it.`,
    })
  }

  if (status === 'in-progress') {
    const lastHeard = lastSignalMs ?? (resolvedTimes.length > 0 ? Math.max(...resolvedTimes) : startMs)
    const silentMs = nowMs - lastHeard
    if (silentMs > STALL_THRESHOLD_MS) {
      notices.push({
        kind: 'stalled',
        text: `No signal from this attempt for ${Math.floor(silentMs / 60_000)}m; it may be stalled.`,
      })
    }
  }

  const segments = buildPhaseSegments(ordered, startMs, endMs, status)
  return { status, startMs, endMs, totalMs: endMs - startMs, segments, events: ordered, notices }
}
