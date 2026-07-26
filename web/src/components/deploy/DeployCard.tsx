import { formatMillis, formatRelativeOffset } from '../../lib/formatters'
import type { DeployStatus, DeployTimeline } from '../../lib/deploy-timeline'
import { PHASE_LABELS, type PhaseId, type PhaseSegment } from '../../lib/deploy-phases'
import { phaseTone, type PhaseColor } from './phase-colors'

export type DeployCardProps = {
  timeline: DeployTimeline
  /** Identifies the attempt, e.g. "attempt-gpu01-91cd". Optional. */
  attemptId?: string
  /** Last signal line, e.g. "last signal 3 s ago · server.artifact_transfer". Optional. */
  lastSignal?: string
}

/** Canonical deploy order. 'untracked' is deliberately absent: it is a gap in
 *  attribution, not a stage the attempt passes through. */
const PHASE_ORDER: PhaseId[] = ['power-on', 'installer-boot', 'inventory-config', 'image-apply', 'reboot-os']

type PhaseState = 'done' | 'running' | 'pending'

type PhaseShare = {
  phaseId: PhaseId
  state: PhaseState
  failed: boolean
  durationMs: number
}

const STATUS_LABEL: Record<Exclude<DeployStatus, 'empty'>, string> = {
  'in-progress': 'IN PROGRESS',
  complete: 'COMPLETE',
  failed: 'FAILED',
}

// Sums segment durations per phase and marks each phase done / running /
// pending. Untracked segments carry real elapsed time, so their duration is
// folded into the phase that was open at that point (the previous ranked
// phase) rather than dropped, keeping the band proportional to the attempt.
function collectPhaseShares(segments: PhaseSegment[]): PhaseShare[] {
  const durations = new Map<PhaseId, number>()
  const failed = new Set<PhaseId>()
  let runningPhase: PhaseId | undefined
  let reachedRank = -1

  for (const segment of segments) {
    const durationMs = Math.max(0, segment.endMs - segment.startMs)
    const rank = PHASE_ORDER.indexOf(segment.phaseId)
    const target = rank >= 0 ? segment.phaseId : PHASE_ORDER[Math.max(0, reachedRank)]
    durations.set(target, (durations.get(target) ?? 0) + durationMs)
    if (rank >= 0) reachedRank = Math.max(reachedRank, rank)
    if (segment.failed) failed.add(target)
    if (segment.kind === 'live' && rank >= 0) runningPhase = segment.phaseId
  }

  const shares: PhaseShare[] = []
  for (let rank = 0; rank <= reachedRank; rank += 1) {
    const phaseId = PHASE_ORDER[rank]
    shares.push({
      phaseId,
      state: phaseId === runningPhase ? 'running' : 'done',
      failed: failed.has(phaseId),
      durationMs: durations.get(phaseId) ?? 0,
    })
  }
  return shares
}

function currentPhaseLabel(shares: PhaseShare[]): string | undefined {
  const running = shares.find((share) => share.state === 'running')
  return running ? PHASE_LABELS[running.phaseId] : undefined
}

export function DeployCard({ timeline, attemptId, lastSignal }: DeployCardProps) {
  const { status, totalMs, segments } = timeline
  if (status === 'empty' || segments.length === 0) return null

  const shares = collectPhaseShares(segments)
  const notStartedCount = PHASE_ORDER.length - shares.length
  const runningLabel = status === 'in-progress' ? currentPhaseLabel(shares) : undefined

  return (
    <div className="border border-line bg-panel p-[14px_16px]">
      <CardHeader status={status} totalMs={totalMs} attemptId={attemptId} runningLabel={runningLabel} />
      <PhaseBandRow shares={shares} notStartedCount={notStartedCount} />
      <PhaseLegend shares={shares} />
      {lastSignal && (
        <p className="m-0 mt-[10px] border-t border-line pt-2 font-mono text-[10px] text-ink-soft">{lastSignal}</p>
      )}
    </div>
  )
}

function CardHeader({
  status,
  totalMs,
  attemptId,
  runningLabel,
}: {
  status: DeployStatus
  totalMs: number
  attemptId?: string
  runningLabel?: string
}) {
  const statusText = STATUS_LABEL[status as Exclude<DeployStatus, 'empty'>]
  const elapsed = formatRelativeOffset(totalMs)
  return (
    <div className="flex items-baseline justify-between gap-[10px]">
      <div className="flex min-w-0 items-baseline gap-[8px]">
        <span className="font-mono text-[10px] font-semibold uppercase tracking-[.14em] text-ink-soft">
          DEPLOY · {statusText}
        </span>
        {runningLabel && (
          <span className="truncate font-mono text-[11px] font-medium text-warn">{runningLabel}</span>
        )}
      </div>
      <span className="whitespace-nowrap font-mono text-[11px] text-ink-soft tabular-nums">
        {elapsed}
        {attemptId ? ` · ${attemptId}` : ''}
      </span>
    </div>
  )
}

function bandStyle(color: PhaseColor, isLast: boolean) {
  return {
    backgroundColor: color.fill,
    borderRightColor: color.border,
    borderRightWidth: isLast ? 0 : 1,
    borderRightStyle: 'solid' as const,
  }
}

function PhaseBandRow({ shares, notStartedCount }: { shares: PhaseShare[]; notStartedCount: number }) {
  // A zero-duration phase would collapse to nothing; every reached phase keeps
  // a minimum share so the band stays readable and legend-aligned.
  const grow = (durationMs: number) => Math.max(durationMs, 1)
  const hasNotStarted = notStartedCount > 0
  return (
    <div className="mt-[10px] flex h-[26px] border border-line" data-testid="deploy-band">
      {shares.map((share, index) => {
        const isLast = !hasNotStarted && index === shares.length - 1
        return (
          <div
            key={share.phaseId}
            data-testid="deploy-band-cell"
            data-phase={share.phaseId}
            data-state={share.failed ? 'failed' : share.state}
            title={`${PHASE_LABELS[share.phaseId]} · ${formatMillis(share.durationMs)}`}
            style={{ flexGrow: grow(share.durationMs), flexBasis: 0, ...bandStyle(phaseTone(share.phaseId, share.state, share.failed), isLast) }}
          />
        )
      })}
      {hasNotStarted && (
        <div
          data-testid="deploy-band-cell"
          data-phase="not-started"
          data-state="pending"
          title={`${notStartedCount} phase${notStartedCount === 1 ? '' : 's'} not started`}
          style={{
            flexGrow: notStartedCount,
            flexBasis: 0,
            ...bandStyle(phaseTone('power-on', 'pending'), true),
          }}
        />
      )}
    </div>
  )
}

function PhaseLegend({ shares }: { shares: PhaseShare[] }) {
  const byPhase = new Map(shares.map((share) => [share.phaseId, share]))
  return (
    <div className="mt-[10px] grid grid-cols-5 gap-x-[10px] gap-y-[4px]">
      {PHASE_ORDER.map((phaseId) => (
        <LegendEntry key={phaseId} phaseId={phaseId} share={byPhase.get(phaseId)} />
      ))}
    </div>
  )
}

function LegendEntry({ phaseId, share }: { phaseId: PhaseId; share?: PhaseShare }) {
  const state: PhaseState = share ? share.state : 'pending'
  const color = phaseTone(phaseId, state, share?.failed ?? false)
  return (
    <div className="flex min-w-0 items-baseline gap-[5px] font-mono text-[10px]" data-testid="deploy-legend-entry">
      <span
        className="inline-block h-[7px] w-[7px] shrink-0 self-center border"
        style={{ backgroundColor: color.fill, borderColor: color.border }}
      />
      <span className="min-w-0 truncate text-ink-soft" title={PHASE_LABELS[phaseId]}>
        {PHASE_LABELS[phaseId]}
      </span>
      <span className={`ml-auto whitespace-nowrap tabular-nums ${share ? 'text-ink' : 'text-ink-soft'}`}>
        {share ? formatMillis(share.durationMs) : '—'}
      </span>
    </div>
  )
}
