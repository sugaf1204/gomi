import { formatDate, formatMillis, formatRelativeOffset } from '../../lib/formatters'
import type { DeployTimeline, ResolvedEvent } from '../../lib/deploy-timeline'
import { segmentAt } from '../../lib/deploy-phases'
import { UNTRACKED_HATCH, segmentColor } from './phase-colors'

type Props = {
  timeline: DeployTimeline
}

// Every reported event of the attempt in chronological order — no truncation.
// The phase dot ties each row back to its segment in the band above.
export function DeployEventTable({ timeline }: Props) {
  const { events, startMs } = timeline
  if (events.length === 0) return null

  return (
    <div className="min-w-0 border border-line bg-panel-2 px-[0.72rem] py-[0.7rem]">
      <div className="flex items-baseline justify-between gap-[0.8rem]">
        <p className="m-0 text-[0.72rem] uppercase text-ink-soft">Events</p>
        <p className="m-0 text-[0.74rem] text-ink-soft tabular-nums">{events.length} reported</p>
      </div>
      <div className="mt-[0.5rem] max-h-[24rem] overflow-auto">
        <table className="w-full border-collapse text-[0.76rem]">
          <thead>
            <tr className="text-left text-[0.68rem] uppercase text-ink-soft">
              <th className="sticky top-0 bg-panel-2 py-[0.3rem] pr-[0.5rem] font-semibold" />
              <th className="sticky top-0 bg-panel-2 py-[0.3rem] pr-[0.5rem] font-semibold">Offset</th>
              <th className="sticky top-0 bg-panel-2 py-[0.3rem] pr-[0.5rem] font-semibold max-sm:hidden">Time</th>
              <th className="sticky top-0 bg-panel-2 py-[0.3rem] pr-[0.5rem] font-semibold">Source</th>
              <th className="sticky top-0 bg-panel-2 py-[0.3rem] pr-[0.5rem] font-semibold">Event</th>
              <th className="sticky top-0 bg-panel-2 py-[0.3rem] text-right font-semibold">Duration</th>
            </tr>
          </thead>
          <tbody>
            {events.map((event) => (
              <EventRow key={event.index} event={event} timeline={timeline} startMs={startMs} />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function eventOffsetLabel(event: ResolvedEvent, startMs?: number) {
  if (event.startMs !== undefined && startMs !== undefined) {
    return formatRelativeOffset(event.startMs - startMs)
  }
  if (event.timeSource === 'monotonic' && event.monotonicSeconds !== undefined) {
    return `boot+${event.monotonicSeconds.toFixed(1)}s`
  }
  return '-'
}

function PhaseDot({ event, timeline }: { event: ResolvedEvent; timeline: DeployTimeline }) {
  const segment = segmentAt(timeline.segments, event.startMs ?? event.endMs)
  if (!segment) return <span className="inline-block h-[0.55rem] w-[0.55rem]" />
  const color = segmentColor(segment.phaseId, segment.failed)
  return (
    <span
      className="inline-block h-[0.55rem] w-[0.55rem] rounded-full border"
      style={{
        backgroundColor: color.fill,
        borderColor: color.border,
        backgroundImage: segment.kind === 'untracked' ? UNTRACKED_HATCH : undefined,
      }}
      title={segment.label}
    />
  )
}

function EventRow({ event, timeline, startMs }: { event: ResolvedEvent; timeline: DeployTimeline; startMs?: number }) {
  const rowClass = event.failed ? 'bg-[#f6dada]' : ''
  return (
    <tr className={`border-t border-line align-baseline ${rowClass}`}>
      <td className="py-[0.28rem] pr-[0.5rem]">
        <PhaseDot event={event} timeline={timeline} />
      </td>
      <td className="py-[0.28rem] pr-[0.5rem] tabular-nums whitespace-nowrap">{eventOffsetLabel(event, startMs)}</td>
      <td className="py-[0.28rem] pr-[0.5rem] tabular-nums whitespace-nowrap text-ink-soft max-sm:hidden">
        {event.startMs !== undefined ? formatDate(new Date(event.startMs).toISOString()) : '-'}
      </td>
      <td className="py-[0.28rem] pr-[0.5rem] text-ink-soft">{event.source}</td>
      <td className="py-[0.28rem] pr-[0.5rem]">
        <span className={event.failed ? 'text-error font-medium' : ''}>{event.name}</span>
        {event.timeSource === 'unresolved' && (
          <span className="ml-[0.4rem] rounded-full bg-[#e8e4df] px-[0.4rem] text-[0.66rem] text-ink-soft">no timestamp</span>
        )}
        {event.message && (
          <span className="block text-[0.7rem] leading-[1.35] text-ink-soft break-anywhere">{event.message}</span>
        )}
      </td>
      <td className="py-[0.28rem] text-right tabular-nums whitespace-nowrap">
        {event.durationMs !== undefined ? formatMillis(event.durationMs) : '-'}
      </td>
    </tr>
  )
}
