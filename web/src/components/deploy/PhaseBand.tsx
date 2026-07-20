import { formatMillis, formatRelativeOffset } from '../../lib/formatters'
import type { DeployTimeline } from '../../lib/deploy-timeline'
import type { PhaseSegment } from '../../lib/deploy-phases'
import { UNTRACKED_HATCH, segmentColor } from './phase-colors'

type Props = {
  timeline: DeployTimeline
}

// One horizontal bar spanning the whole attempt, partitioned into contiguous
// segments: every millisecond belongs to exactly one segment, and the
// reconciliation line below proves that the parts sum to the total.
export function PhaseBand({ timeline }: Props) {
  const { startMs, totalMs, segments } = timeline
  if (startMs === undefined || totalMs <= 0 || segments.length === 0) return null

  const tickCount = 5
  const ticks = Array.from({ length: tickCount + 1 }, (_, i) => ({
    pct: (i / tickCount) * 100,
    label: formatRelativeOffset((totalMs * i) / tickCount),
  }))
  const segmentSumMs = segments.reduce((sum, s) => sum + (s.endMs - s.startMs), 0)

  return (
    <div className="min-w-0 border border-line bg-panel-2 px-[0.72rem] py-[0.7rem]">
      <p className="m-0 mb-[1.1rem] text-[0.72rem] uppercase text-ink-soft">Deploy Timeline</p>

      <div className="relative mb-[0.25rem] h-[0.9rem]">
        {ticks.map((tick) => (
          <span
            key={tick.pct}
            className="absolute text-[0.68rem] text-ink-soft tabular-nums"
            style={{ left: `${tick.pct}%`, transform: tick.pct === 0 ? 'none' : tick.pct === 100 ? 'translateX(-100%)' : 'translateX(-50%)' }}
          >
            {tick.label}
          </span>
        ))}
      </div>

      <div className="relative h-[1.9rem] w-full overflow-hidden rounded-sm border border-line">
        {segments.map((segment, idx) => (
          <BandSegment key={idx} segment={segment} startMs={startMs} totalMs={totalMs} />
        ))}
      </div>

      <div className="mt-[0.6rem] grid gap-[0.15rem]">
        {segments.map((segment, idx) => (
          <SegmentRow key={idx} segment={segment} startMs={startMs} totalMs={totalMs} />
        ))}
      </div>

      <p className="m-0 mt-[0.55rem] border-t border-line pt-[0.45rem] text-[0.76rem] text-ink-soft tabular-nums">
        Total {formatMillis(totalMs)} = {segments.length} segment{segments.length === 1 ? '' : 's'} ({formatMillis(segmentSumMs)})
      </p>
    </div>
  )
}

function segmentTitle(segment: PhaseSegment, startMs: number) {
  const range = `${formatRelativeOffset(segment.startMs - startMs)} → ${formatRelativeOffset(segment.endMs - startMs)}`
  const bounds = `${segment.fromLabel} → ${segment.toLabel}`
  return `${segment.label} (${range}, ${formatMillis(segment.endMs - segment.startMs)})\n${bounds}`
}

function BandSegment({ segment, startMs, totalMs }: { segment: PhaseSegment; startMs: number; totalMs: number }) {
  const left = ((segment.startMs - startMs) / totalMs) * 100
  const width = ((segment.endMs - segment.startMs) / totalMs) * 100
  const color = segmentColor(segment.phaseId, segment.failed)
  return (
    <div
      className={`absolute top-0 h-full border-r last:border-r-0 ${segment.kind === 'live' ? 'animate-pulse' : ''}`}
      style={{
        left: `${left}%`,
        width: `${width}%`,
        backgroundColor: color.fill,
        borderRightColor: color.border,
        backgroundImage: segment.kind === 'untracked' ? UNTRACKED_HATCH : undefined,
      }}
      title={segmentTitle(segment, startMs)}
    />
  )
}

function SegmentRow({ segment, startMs, totalMs }: { segment: PhaseSegment; startMs: number; totalMs: number }) {
  const color = segmentColor(segment.phaseId, segment.failed)
  const durationMs = segment.endMs - segment.startMs
  const share = Math.round((durationMs / totalMs) * 100)
  const detail =
    segment.kind === 'untracked'
      ? `no events between ${segment.fromLabel} and ${segment.toLabel}`
      : `${segment.fromLabel} → ${segment.toLabel}`
  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)_auto_auto] items-baseline gap-[0.5rem] text-[0.78rem]">
      <span
        className="inline-block h-[0.62rem] w-[0.62rem] self-center rounded-[2px] border"
        style={{
          backgroundColor: color.fill,
          borderColor: color.border,
          backgroundImage: segment.kind === 'untracked' ? UNTRACKED_HATCH : undefined,
        }}
      />
      <span className="min-w-0 truncate" title={detail}>
        {segment.label}
        {segment.kind === 'live' && <span className="text-ink-soft"> — in progress</span>}
        {segment.failed && <span className="text-error"> — failed</span>}
        <span className="text-ink-soft text-[0.72rem]"> · {detail}</span>
      </span>
      <span className="text-ink-soft tabular-nums text-[0.72rem] whitespace-nowrap">
        {formatRelativeOffset(segment.startMs - startMs)} → {formatRelativeOffset(segment.endMs - startMs)}
      </span>
      <span className="tabular-nums whitespace-nowrap text-right min-w-[4.5rem]">
        {formatMillis(durationMs)} <span className="text-ink-soft text-[0.72rem]">({share}%)</span>
      </span>
    </div>
  )
}
