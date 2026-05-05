import { formatDate } from '../../../lib/formatters'
import type { AuditEvent } from '../../../types'

type Props = {
  auditEvents: AuditEvent[]
}

export function ActivityTab({ auditEvents }: Props) {
  return (
    <div className="pt-[0.65rem]">
      <div className="grid gap-[0.45rem] max-h-[330px] overflow-auto pr-[0.2rem]">
        {auditEvents.map((event) => (
          <article key={event.id} className="border-0 border-b border-line bg-transparent py-[0.58rem] px-[0.2rem]">
            <header className="flex justify-between gap-[0.6rem] items-baseline mb-[0.28rem]">
              <strong>{event.action}</strong>
              <span>{event.result.toUpperCase()}</span>
            </header>
            <p className="m-0 text-ink-soft text-[0.86rem]">{event.actor} - {formatDate(event.createdAt)}</p>
            {event.message && <code className="block mt-[0.38rem] text-[#7a4c2a] text-[0.8rem]">{event.message}</code>}
          </article>
        ))}
        {auditEvents.length === 0 && <p className="m-0 text-ink-soft">No activity for this machine</p>}
      </div>
    </div>
  )
}
