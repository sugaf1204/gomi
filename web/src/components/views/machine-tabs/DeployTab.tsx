import { useMemo } from 'react'
import { formatDate } from '../../../lib/formatters'
import { deriveDeployTimeline } from '../../../lib/deploy-timeline'
import { DeployCard } from '../../deploy/DeployCard'
import { DeployNotices } from '../../deploy/DeployNotices'
import { DeployEventTable } from '../../deploy/DeployEventTable'
import { ActivityTab } from './ActivityTab'
import type { AuditEvent, Machine } from '../../../types'

type Props = {
  machine: Machine
  auditEvents: AuditEvent[]
}

export function DeployTab({ machine, auditEvents }: Props) {
  const provision = machine.provision
  // The 5s polling refresh replaces the machine object, which re-derives the
  // timeline and advances the live segment of an in-progress attempt. Only the
  // tone changes; nothing moves.
  const timeline = useMemo(() => deriveDeployTimeline(provision, Date.now()), [provision])

  if (timeline.status === 'empty' && timeline.events.length === 0) {
    return (
      <div className="pt-[14px]">
        <p className="m-0 font-mono text-[11.5px] text-ink-soft">No deploy attempt has been recorded for this machine.</p>
      </div>
    )
  }

  const lastEvent = timeline.events.at(-1)

  return (
    <div className="grid gap-[18px] pt-[14px]">
      <DeployCard
        timeline={timeline}
        attemptId={provision?.attemptId}
        lastSignal={lastEvent ? `last signal · ${lastEvent.name}` : undefined}
      />
      <DeployNotices notices={timeline.notices} />
      <p className="m-0 font-mono text-[10.5px] text-ink-soft">
        {provision?.startedAt ? `attempt started ${formatDate(provision.startedAt)}` : 'attempt start time not recorded'}
        {provision?.requestedBy ? ` by ${provision.requestedBy}` : ''}
      </p>
      <DeployEventTable timeline={timeline} />
      <ActivityTab auditEvents={auditEvents} />
    </div>
  )
}
