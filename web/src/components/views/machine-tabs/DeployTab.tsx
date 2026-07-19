import { useMemo } from 'react'
import { formatDate } from '../../../lib/formatters'
import { deriveDeployTimeline } from '../../../lib/deploy-timeline'
import { DeploySummary } from '../../deploy/DeploySummary'
import { DeployNotices } from '../../deploy/DeployNotices'
import { PhaseBand } from '../../deploy/PhaseBand'
import { DeployEventTable } from '../../deploy/DeployEventTable'
import type { Machine } from '../../../types'

type Props = {
  machine: Machine
}

export function DeployTab({ machine }: Props) {
  const provision = machine.provision
  // The 5s polling refresh replaces the machine object, which re-derives the
  // timeline and advances the live segment of an in-progress attempt.
  const timeline = useMemo(() => deriveDeployTimeline(provision, Date.now()), [provision])

  if (timeline.status === 'empty' && timeline.events.length === 0) {
    return (
      <div className="pt-[0.65rem]">
        <p className="m-0 text-[0.86rem] text-ink-soft">No deploy attempt has been recorded for this machine.</p>
      </div>
    )
  }

  return (
    <div className="grid gap-[1rem] pt-[0.65rem]">
      <div className="flex items-baseline justify-between gap-[0.8rem] max-sm:grid">
        <p className="m-0 text-[0.86rem] text-ink-soft">
          {provision?.startedAt ? `Attempt started ${formatDate(provision.startedAt)}` : 'Attempt start time not recorded'}
          {provision?.requestedBy ? ` by ${provision.requestedBy}` : ''}
        </p>
        {provision?.attemptId && (
          <p className="m-0 text-[0.74rem] text-ink-soft tabular-nums break-anywhere">{provision.attemptId}</p>
        )}
      </div>
      <DeploySummary timeline={timeline} provision={provision} />
      <DeployNotices notices={timeline.notices} />
      <PhaseBand timeline={timeline} />
      <DeployEventTable timeline={timeline} />
    </div>
  )
}
