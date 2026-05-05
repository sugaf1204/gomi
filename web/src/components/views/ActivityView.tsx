import type { ActivityItem, ActivitySummary, ActivityType } from '../../app-types'
import { formatDate, phaseClass } from '../../lib/formatters'
import type { Machine } from '../../types'

const activityBadgeColor: Record<string, string> = {
  job: 'bg-[#e0ecf8] text-[#2a5a8a]',
  audit: 'bg-[#f0e8d8] text-[#7a5c2a]',
}

export type ActivityViewProps = {
  activityTypeFilter: ActivityType
  onActivityTypeChange: (value: ActivityType) => void
  activityMachineFilter: string
  onActivityMachineChange: (value: string) => void
  machines: Machine[]
  filteredItems: ActivityItem[]
  summary: ActivitySummary
}

export function ActivityView({
  activityTypeFilter,
  onActivityTypeChange,
  activityMachineFilter,
  onActivityMachineChange,
  machines,
  filteredItems,
  summary
}: ActivityViewProps) {
  return (
    <section className="min-h-0 grid grid-cols-[minmax(0,1fr)_320px] gap-[0.9rem] items-start lg:grid-cols-[minmax(0,1fr)_320px] max-lg:grid-cols-1">
      <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.65rem] self-start">
        <div className="flex justify-between items-center gap-4 max-sm:flex-col max-sm:items-start">
          <h2 className="text-[1.4rem]">Activity Timeline</h2>
          <div className="flex gap-[0.4rem]">
            <select value={activityTypeFilter} onChange={(e) => onActivityTypeChange(e.target.value as ActivityType)}>
              <option value="all">All types</option>
              <option value="audit">Audit only</option>
            </select>
            <select value={activityMachineFilter} onChange={(e) => onActivityMachineChange(e.target.value)}>
              <option value="">All machines</option>
              {machines.map((machine) => (
                <option key={machine.name} value={machine.name}>{machine.name}</option>
              ))}
            </select>
          </div>
        </div>
        <div className="grid gap-[0.45rem] max-h-[62vh] overflow-auto pr-[0.2rem]">
          {filteredItems.map((item) => (
            <article key={item.id} className="border-0 border-b border-line bg-transparent py-[0.58rem] px-[0.2rem]">
              <header className="flex justify-between gap-[0.6rem] items-baseline mb-[0.28rem]">
                <div className="flex gap-[0.45rem] items-baseline">
                  <span className={`inline-flex items-center text-[0.66rem] font-semibold uppercase tracking-[0.04em] py-[0.12rem] px-[0.4rem] rounded-full ${activityBadgeColor[item.type] ?? 'bg-[#e8e8e8] text-[#555]'}`}>{item.type}</span>
                  <strong>{item.action}</strong>
                </div>
                <span className={phaseClass(item.result)}>{item.result.toUpperCase()}</span>
              </header>
              <p className="m-0 text-ink-soft text-[0.86rem]">{item.machine || '-'} - {item.actor}</p>
              <p className="m-0 text-ink-soft text-[0.86rem] mt-[0.2rem]">{formatDate(item.timestamp)}</p>
              {item.message && <code className="block mt-[0.38rem] text-[#7a4c2a] text-[0.8rem]">{item.message}</code>}
            </article>
          ))}
          {filteredItems.length === 0 && <p className="m-0 text-ink-soft">No activity records found</p>}
        </div>
      </section>

      <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
        <h3 className="mb-[0.58rem] text-[1.05rem]">Summary</h3>
        <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
          <dt className="text-ink-soft text-[0.84rem]">Total</dt><dd className="m-0">{summary.total}</dd>
          <dt className="text-ink-soft text-[0.84rem]">Audit Events</dt><dd className="m-0">{summary.auditCount}</dd>
          <dt className="text-ink-soft text-[0.84rem]">Success</dt><dd className="m-0">{summary.success}</dd>
          <dt className="text-ink-soft text-[0.84rem]">Failure</dt><dd className="m-0">{summary.failure}</dd>
        </dl>
      </section>
    </section>
  )
}
