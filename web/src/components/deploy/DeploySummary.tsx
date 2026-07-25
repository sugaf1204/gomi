import { formatMillis } from '../../lib/formatters'
import type { DeployTimeline } from '../../lib/deploy-timeline'
import type { ProvisionProgress } from '../../types'

type Props = {
  timeline: DeployTimeline
  provision?: ProvisionProgress
}

const STATUS_STYLES: Record<string, string> = {
  complete: 'bg-ok-bg text-ok',
  'in-progress': 'bg-warn-bg text-warn',
  failed: 'bg-error-bg text-error',
  empty: 'bg-neutral-bg text-ink-soft',
}

const STATUS_LABELS: Record<string, string> = {
  complete: 'Completed',
  'in-progress': 'In progress',
  failed: 'Failed',
  empty: 'No attempt',
}

export function DeploySummary({ timeline, provision }: Props) {
  const failedCount = timeline.events.filter((e) => e.failed).length
  const slowest = [...timeline.segments]
    .filter((s) => s.phaseId !== 'untracked')
    .sort((a, b) => b.endMs - b.startMs - (a.endMs - a.startMs))[0]
  const stalled = timeline.notices.some((n) => n.kind === 'stalled')

  const cards = [
    {
      label: 'Status',
      value: (
        <span
          className={`inline-flex w-fit items-center rounded-full px-2 py-0.5 font-ui text-[0.71rem] font-semibold ${STATUS_STYLES[timeline.status]}`}
        >
          {stalled ? 'Stalled?' : STATUS_LABELS[timeline.status]}
        </span>
      ),
      detail: timeline.status === 'failed' ? provision?.failureReason : provision?.trigger && `trigger: ${provision.trigger}`,
    },
    {
      label: 'Total Duration',
      value: timeline.totalMs > 0 ? formatMillis(timeline.totalMs) : '-',
      detail: timeline.status === 'in-progress' ? 'still running' : undefined,
    },
    {
      label: 'Events',
      value: String(timeline.events.length),
      detail: failedCount > 0 ? `${failedCount} failed` : undefined,
    },
    {
      label: 'Slowest Phase',
      value: slowest ? formatMillis(slowest.endMs - slowest.startMs) : '-',
      detail: slowest?.label,
    },
  ]

  return (
    <div className="grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-[0.55rem]">
      {cards.map((card) => (
        <div key={card.label} className="border border-line bg-panel-2 px-[0.65rem] py-[0.55rem]">
          <p className="m-0 text-[0.72rem] uppercase text-ink-soft">{card.label}</p>
          <p className="m-0 mt-[0.18rem] text-[1rem] font-medium tabular-nums">{card.value}</p>
          {card.detail && (
            <p className="m-0 mt-[0.16rem] text-[0.75rem] leading-[1.35] text-ink-soft break-anywhere">{card.detail}</p>
          )}
        </div>
      ))}
    </div>
  )
}
