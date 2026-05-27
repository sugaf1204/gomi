import { formatDate } from '../../../lib/formatters'
import type { Machine, ProvisionTiming } from '../../../types'

type Props = {
  machine: Machine
}

export function DeployTab({ machine }: Props) {
  const timings = machine.provision?.timings ?? []
  const visibleTimings = timings.slice(-30)
  const span = computeTimeSpan(machine, visibleTimings)

  return (
    <div className="pt-[0.65rem] grid gap-[1rem]">
      <SummaryCards machine={machine} timings={timings} />
      {visibleTimings.length > 0 ? (
        <GanttChart machine={machine} timings={visibleTimings} span={span} />
      ) : (
        <p className="m-0 text-ink-soft text-[0.86rem]">No deploy timing has been reported for the current provisioning attempt.</p>
      )}
    </div>
  )
}

type TimeSpan = { originMs: number; totalMs: number }

function computeTimeSpan(machine: Machine, timings: ProvisionTiming[]): TimeSpan {
  const startMs = machine.provision?.startedAt ? Date.parse(machine.provision.startedAt) : undefined
  let minMs = startMs && Number.isFinite(startMs) ? startMs : Infinity
  let maxMs = startMs && Number.isFinite(startMs) ? startMs : -Infinity

  for (const t of timings) {
    const s = resolveStartMs(t)
    const e = resolveEndMs(t)
    if (s !== undefined) { minMs = Math.min(minMs, s); maxMs = Math.max(maxMs, s) }
    if (e !== undefined) { minMs = Math.min(minMs, e); maxMs = Math.max(maxMs, e) }
  }

  if (!Number.isFinite(minMs) || !Number.isFinite(maxMs) || maxMs <= minMs) {
    return { originMs: 0, totalMs: 1 }
  }
  const pad = Math.max((maxMs - minMs) * 0.03, 500)
  return { originMs: minMs, totalMs: maxMs - minMs + pad }
}

function resolveStartMs(t: ProvisionTiming): number | undefined {
  const raw = t.startedAt ?? t.timestamp
  if (!raw) return undefined
  const ms = Date.parse(raw)
  return Number.isFinite(ms) ? ms : undefined
}

function resolveEndMs(t: ProvisionTiming): number | undefined {
  const raw = t.finishedAt ?? t.timestamp
  if (!raw) return undefined
  const ms = Date.parse(raw)
  return Number.isFinite(ms) ? ms : undefined
}

function barPosition(t: ProvisionTiming, span: TimeSpan) {
  const startMs = resolveStartMs(t)
  const endMs = resolveEndMs(t)
  if (startMs === undefined) return { left: 0, width: 0 }

  const left = ((startMs - span.originMs) / span.totalMs) * 100
  if (endMs !== undefined && endMs > startMs) {
    const width = ((endMs - startMs) / span.totalMs) * 100
    return { left, width: Math.max(width, 0.4) }
  }
  if (typeof t.durationMs === 'number' && t.durationMs > 0) {
    const width = (t.durationMs / span.totalMs) * 100
    return { left, width: Math.max(width, 0.4) }
  }
  return { left, width: 0.4 }
}

function SummaryCards({ machine, timings }: { machine: Machine; timings: ProvisionTiming[] }) {
  const slowest = [...timings]
    .filter((e) => typeof e.durationMs === 'number' && e.durationMs > 0)
    .sort((a, b) => (b.durationMs ?? 0) - (a.durationMs ?? 0))[0]
  const attemptEnd = machine.provision?.completedAt ?? machine.provision?.finishedAt ?? latestTimestamp(timings)
  const attemptDuration = durationBetween(machine.provision?.startedAt, attemptEnd)
  const eventCount = timings.length
  const failureCount = timings.filter(isFailure).length

  const cards = [
    { label: 'Total Duration', value: attemptDuration ?? '-', detail: machine.provision?.trigger },
    { label: 'Events', value: String(eventCount), detail: failureCount > 0 ? `${failureCount} failed` : undefined },
    { label: 'Slowest Step', value: slowest ? formatMillis(slowest.durationMs!) : '-', detail: slowest ? formatName(slowest.name) : undefined },
  ]

  return (
    <div className="grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-[0.55rem]">
      {cards.map((c) => (
        <div key={c.label} className="border border-line bg-surface-muted px-[0.65rem] py-[0.55rem]">
          <p className="m-0 text-[0.72rem] uppercase text-ink-soft">{c.label}</p>
          <p className="m-0 mt-[0.18rem] text-[1rem] font-medium tabular-nums">{c.value}</p>
          {c.detail && <p className="m-0 mt-[0.16rem] text-[0.75rem] leading-[1.35] text-ink-soft break-anywhere">{c.detail}</p>}
        </div>
      ))}
    </div>
  )
}

function GanttChart({ machine, timings, span }: { machine: Machine; timings: ProvisionTiming[]; span: TimeSpan }) {
  const tickCount = 5
  const ticks = Array.from({ length: tickCount + 1 }, (_, i) => {
    const ms = span.originMs + (span.totalMs * i) / tickCount
    return { pct: (i / tickCount) * 100, label: formatTickLabel(ms, span.originMs) }
  })

  const groups = groupBySource(timings)

  return (
    <div className="border border-line bg-panel-2 px-[0.72rem] py-[0.7rem]">
      <div className="flex items-start justify-between gap-[0.8rem] mb-[0.65rem] max-sm:grid">
        <div className="min-w-0">
          <p className="m-0 text-[0.72rem] uppercase text-ink-soft">Deploy Gantt</p>
          <p className="m-0 mt-[0.16rem] text-[0.88rem] leading-[1.35] text-ink-soft">
            Showing the latest {timings.length} events as a Gantt chart.
          </p>
        </div>
        <div className="min-w-0 text-right max-sm:text-left">
          <p className="m-0 text-[0.72rem] uppercase text-ink-soft">Started</p>
          <p className="m-0 mt-[0.16rem] text-[0.84rem] leading-[1.35] text-ink-soft tabular-nums">
            {machine.provision?.startedAt ? formatDate(machine.provision.startedAt) : '-'}
          </p>
        </div>
      </div>

      <div className="relative mb-[0.2rem]">
        <div className="flex justify-between text-[0.68rem] text-ink-soft tabular-nums">
          {ticks.map((tick) => (
            <span key={tick.pct} style={{ position: 'absolute', left: `${tick.pct}%`, transform: 'translateX(-50%)' }}>
              {tick.label}
            </span>
          ))}
        </div>
      </div>

      <div className="mt-[1.4rem] border-t border-line">
        {groups.map((group) => (
          <div key={group.source} className="border-b border-line">
            <div className="py-[0.4rem] px-[0.15rem]">
              <p className="m-0 text-[0.72rem] font-semibold uppercase tracking-[0.04em] text-ink-soft mb-[0.3rem]">{group.source}</p>
              {group.items.map((event, idx) => (
                <GanttRow key={`${event.name}-${event.timestamp ?? event.finishedAt ?? idx}`} event={event} span={span} />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function GanttRow({ event, span }: { event: ProvisionTiming; span: TimeSpan }) {
  const pos = barPosition(event, span)
  const failed = isFailure(event)
  const success = event.result?.toLowerCase() === 'success'

  const barColor = failed
    ? 'bg-[#e8a0a0] border-error'
    : success
      ? 'bg-[#a0d8c0] border-[#3a9a6e]'
      : 'bg-[#c8d0e0] border-[#8090b0]'

  return (
    <div className="grid grid-cols-[140px_minmax(0,1fr)_80px] items-center gap-[0.4rem] py-[0.22rem] max-sm:grid-cols-[100px_minmax(0,1fr)_60px]">
      <div className="min-w-0 truncate text-[0.76rem]" title={formatName(event.name)}>
        {formatName(event.name)}
      </div>
      <div className="relative h-[1.1rem] bg-[rgba(0,0,0,0.03)] rounded-sm overflow-hidden">
        <div
          className={`absolute top-0 h-full rounded-sm border ${barColor} min-w-[3px]`}
          style={{ left: `${pos.left}%`, width: `${pos.width}%` }}
          title={`${formatName(event.name)} — ${formatDuration(event)}`}
        />
      </div>
      <div className="text-right text-[0.72rem] tabular-nums text-ink-soft whitespace-nowrap">
        {formatDuration(event)}
      </div>
    </div>
  )
}

type SourceGroup = { source: string; items: ProvisionTiming[] }

function groupBySource(timings: ProvisionTiming[]): SourceGroup[] {
  const map = new Map<string, ProvisionTiming[]>()
  for (const t of timings) {
    const src = t.source || 'unknown'
    const arr = map.get(src)
    if (arr) arr.push(t)
    else map.set(src, [t])
  }
  return Array.from(map.entries()).map(([source, items]) => ({ source, items }))
}

function latestTimestamp(timings: ProvisionTiming[]) {
  for (let i = timings.length - 1; i >= 0; i -= 1) {
    const ts = timings[i].finishedAt ?? timings[i].timestamp ?? timings[i].startedAt
    if (ts) return ts
  }
  return undefined
}

function isFailure(event: ProvisionTiming) {
  return event.result?.toLowerCase() === 'failure' || event.result?.toLowerCase() === 'failed' || event.eventType?.toLowerCase() === 'failure'
}

function durationBetween(start?: string, end?: string) {
  if (!start || !end) return undefined
  const s = Date.parse(start)
  const e = Date.parse(end)
  if (!Number.isFinite(s) || !Number.isFinite(e) || e < s) return undefined
  return formatMillis(e - s)
}

function formatDuration(event: ProvisionTiming) {
  if (typeof event.durationMs === 'number' && Number.isFinite(event.durationMs) && event.durationMs > 0) {
    return formatMillis(event.durationMs)
  }
  return '-'
}

function formatMillis(ms: number) {
  if (ms < 1000) return `${ms} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`
  return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`
}

function formatName(name: string) {
  return name
    .replace(/^cmd-install\/stage-/, 'cmd-install/')
    .replace(/^runner\./, 'runner / ')
    .replace(/^server\./, 'server / ')
    .replace(/^initramfs\./, 'initramfs / ')
    .replaceAll('.', ' / ')
    .replaceAll('_', ' ')
}

function formatTickLabel(ms: number, originMs: number) {
  const delta = ms - originMs
  if (delta <= 0) return '0s'
  if (delta < 60_000) return `${(delta / 1000).toFixed(0)}s`
  return `${Math.floor(delta / 60_000)}m${Math.round((delta % 60_000) / 1000)}s`
}
