import { formatDate, formatPowerControlMethod, powerStateClass, powerStateLabel } from '../../../lib/formatters'
import type { Machine, ProvisionTiming } from '../../../types'

type Props = {
  machine: Machine
}

export function InfoTab({ machine }: Props) {
  const timings = machine.provision?.timings ?? []
  const visibleTimings = timings.slice(-24)
  const summary = buildTimingSummary(machine, timings)
  const latestEvent = visibleTimings[visibleTimings.length - 1]

  return (
    <div className="pt-[0.65rem] grid gap-[1rem]">
      <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem] max-sm:grid-cols-[110px_minmax(0,1fr)]">
        <dt className="text-ink-soft text-[0.84rem]">Hostname</dt>
        <dd className="m-0">{machine.hostname}</dd>
        <dt className="text-ink-soft text-[0.84rem]">MAC Address</dt>
        <dd className="m-0">{machine.mac}</dd>
        <dt className="text-ink-soft text-[0.84rem]">IP Address</dt>
        <dd className="m-0">{machine.ip || '-'}</dd>
        <dt className="text-ink-soft text-[0.84rem]">Architecture</dt>
        <dd className="m-0">{machine.arch}</dd>
        <dt className="text-ink-soft text-[0.84rem]">Firmware</dt>
        <dd className="m-0">{machine.firmware.toUpperCase()}</dd>
        <dt className="text-ink-soft text-[0.84rem]">Phase</dt>
        <dd className="m-0">{machine.phase}</dd>
        <dt className="text-ink-soft text-[0.84rem]">Power State</dt>
        <dd className="m-0">
          {machine.powerState ? (
            <span className={powerStateClass(machine.powerState)}>{powerStateLabel(machine.powerState)}</span>
          ) : (
            '-'
          )}
        </dd>
        <dt className="text-ink-soft text-[0.84rem]">Power Method</dt>
        <dd className="m-0">{formatPowerControlMethod(machine.power)}</dd>
        <dt className="text-ink-soft text-[0.84rem]">Provision Status</dt>
        <dd className="m-0">{machine.provision?.message || '-'}</dd>
        <dt className="text-ink-soft text-[0.84rem]">Last Error</dt>
        <dd className="m-0">
          {machine.lastError ? (
            <code className="inline-block max-w-full font-mono text-[0.82rem] leading-[1.45] whitespace-pre-wrap break-anywhere border border-line bg-[rgba(241,237,228,0.95)] text-[#6e2d2d] py-[0.22rem] px-[0.42rem]">
              {machine.lastError}
            </code>
          ) : (
            '-'
          )}
        </dd>
      </dl>

      <section className="border-0 border-t border-line pt-[0.82rem]">
        <div className="flex items-baseline justify-between gap-[0.65rem] mb-[0.5rem]">
          <h4 className="m-0 text-[0.94rem] font-medium text-ink-soft">Deploy Timing</h4>
          <span className="text-[0.78rem] text-ink-soft">{timings.length ? `${timings.length} events` : 'No events'}</span>
        </div>
        {visibleTimings.length > 0 ? (
          <div className="grid gap-[0.75rem]">
            <div className="grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-[0.55rem]">
              {summary.map((item) => (
                <div key={item.label} className="border border-line bg-surface-muted px-[0.65rem] py-[0.55rem]">
                  <p className="m-0 text-[0.72rem] uppercase text-ink-soft">{item.label}</p>
                  <p className="m-0 mt-[0.18rem] text-[1rem] font-medium tabular-nums">{item.value}</p>
                  {item.detail && <p className="m-0 mt-[0.16rem] text-[0.75rem] leading-[1.35] text-ink-soft break-anywhere">{item.detail}</p>}
                </div>
              ))}
            </div>
            <div className="border border-line bg-panel-2 px-[0.72rem] py-[0.7rem]">
              <div className="flex items-start justify-between gap-[0.8rem] mb-[0.65rem] max-sm:grid">
                <div className="min-w-0">
                  <p className="m-0 text-[0.72rem] uppercase text-ink-soft">Timeline</p>
                  <p className="m-0 mt-[0.16rem] text-[0.88rem] leading-[1.35] text-ink-soft">
                    Oldest to newest, showing the latest {visibleTimings.length} deploy events.
                  </p>
                </div>
                <div className="min-w-0 text-right max-sm:text-left">
                  <p className="m-0 text-[0.72rem] uppercase text-ink-soft">Latest</p>
                  <p className="m-0 mt-[0.16rem] text-[0.84rem] leading-[1.35] text-ink-soft break-anywhere">
                    {latestEvent ? `${formatTimingName(latestEvent.name)} (${formatTimingTime(latestEvent)})` : '-'}
                  </p>
                </div>
              </div>
              <ol className="m-0 grid max-h-[360px] list-none gap-0 overflow-y-auto p-0 pr-[0.15rem]">
                {visibleTimings.map((event, index) => (
                  <li
                    key={`${event.source ?? 'unknown'}-${event.name}-${event.timestamp ?? event.finishedAt ?? index}`}
                    className="grid grid-cols-[4.25rem_minmax(0,1fr)] gap-[0.55rem] border-0 border-t border-line py-[0.52rem] first:border-t-0 first:pt-0 last:pb-0 max-sm:grid-cols-[3.9rem_minmax(0,1fr)]"
                  >
                    <div className="whitespace-nowrap pt-[0.1rem] text-right font-mono text-[0.76rem] tabular-nums text-ink-soft">
                      {formatTimingOffset(machine, event)}
                    </div>
                    <div className="relative min-w-0 pl-[0.7rem]">
                      <span className={timingMarkerClass(event)} aria-hidden="true" />
                      {index < visibleTimings.length - 1 && <span className="absolute left-[0.19rem] top-[1rem] bottom-[-0.55rem] w-px bg-line-strong" aria-hidden="true" />}
                      <div className="min-w-0">
                        <div className="flex min-w-0 flex-wrap items-baseline gap-x-[0.45rem] gap-y-[0.12rem]">
                          <p className="m-0 min-w-0 break-anywhere text-[0.86rem] font-medium">{formatTimingName(event.name)}</p>
                          <span className="text-[0.76rem] text-ink-soft">{event.source || 'unknown'}</span>
                          <span className={timingResultClass(event)}>{event.result || event.eventType || 'event'}</span>
                        </div>
                        {event.message && <p className="m-0 mt-[0.12rem] break-anywhere text-[0.76rem] leading-[1.35] text-ink-soft">{event.message}</p>}
                        <div className="mt-[0.18rem] flex flex-wrap gap-x-[0.7rem] gap-y-[0.12rem] text-[0.76rem] text-ink-soft">
                          <span className="tabular-nums">Duration {formatDuration(event)}</span>
                          <span className="tabular-nums">{formatTimingTime(event)}</span>
                        </div>
                      </div>
                    </div>
                  </li>
                ))}
              </ol>
            </div>
          </div>
        ) : (
          <p className="m-0 text-ink-soft text-[0.86rem]">No deploy timing has been reported for the current provisioning attempt.</p>
        )}
      </section>
    </div>
  )
}

function buildTimingSummary(machine: Machine, timings: ProvisionTiming[]) {
  const slowest = [...timings]
    .filter((event) => typeof event.durationMs === 'number' && event.durationMs > 0)
    .sort((a, b) => (b.durationMs ?? 0) - (a.durationMs ?? 0))[0]
  const inventoryStore = latestTiming(timings, 'server.inventory.store') ?? latestTiming(timings, 'runner.inventory')
  const imageTransfer = latestTiming(timings, 'server.file_transfer') ?? latestTiming(timings, 'server.artifact_transfer')
  const curtinInstall = latestTiming(timings, 'runner.curtin_install') ?? latestTiming(timings, 'cmd-install')
  const attemptEnd = machine.provision?.completedAt ?? machine.provision?.finishedAt ?? latestTimingTimestamp(timings)
  const attemptDuration = durationBetween(machine.provision?.startedAt, attemptEnd)

  return [
    { label: 'Attempt', value: attemptDuration ?? '-', detail: machine.provision?.trigger || undefined },
    { label: 'Inventory Store', value: inventoryStore ? formatDuration(inventoryStore) : '-', detail: inventoryStore ? formatTimingName(inventoryStore.name) : undefined },
    { label: 'Image Transfer', value: imageTransfer ? formatDuration(imageTransfer) : '-', detail: imageTransfer?.message },
    { label: 'Curtin Install', value: curtinInstall ? formatDuration(curtinInstall) : '-', detail: curtinInstall?.result },
    { label: 'Slowest', value: slowest ? formatDuration(slowest) : '-', detail: slowest ? formatTimingName(slowest.name) : undefined },
  ]
}

function latestTiming(timings: ProvisionTiming[], name: string) {
  for (let index = timings.length - 1; index >= 0; index -= 1) {
    if (timings[index].name === name) return timings[index]
  }
  return undefined
}

function latestTimingTimestamp(timings: ProvisionTiming[]) {
  for (let index = timings.length - 1; index >= 0; index -= 1) {
    const timestamp = timings[index].finishedAt ?? timings[index].timestamp ?? timings[index].startedAt
    if (timestamp) return timestamp
  }
  return undefined
}

function isFailure(event: ProvisionTiming) {
  return event.result?.toLowerCase() === 'failure' || event.result?.toLowerCase() === 'failed' || event.eventType?.toLowerCase() === 'failure'
}

function durationBetween(start?: string, end?: string) {
  if (!start || !end) return undefined
  const startMs = Date.parse(start)
  const endMs = Date.parse(end)
  if (!Number.isFinite(startMs) || !Number.isFinite(endMs) || endMs < startMs) return undefined
  return formatMillis(endMs - startMs)
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

function formatTimingTime(event: ProvisionTiming) {
  return formatDate(event.finishedAt ?? event.timestamp ?? event.startedAt)
}

function formatTimingName(name: string) {
  return name
    .replace(/^cmd-install\/stage-/, 'cmd-install/')
    .replace(/^runner\./, 'runner / ')
    .replace(/^server\./, 'server / ')
    .replace(/^initramfs\./, 'initramfs / ')
    .replaceAll('.', ' / ')
    .replaceAll('_', ' ')
}

function formatTimingOffset(machine: Machine, event: ProvisionTiming) {
  if (typeof event.monotonicSeconds === 'number' && Number.isFinite(event.monotonicSeconds)) {
    return `T+${event.monotonicSeconds.toFixed(1)}s`
  }
  const eventTime = event.finishedAt ?? event.timestamp ?? event.startedAt
  const start = machine.provision?.startedAt
  if (!start || !eventTime) return '--'
  const startMs = Date.parse(start)
  const eventMs = Date.parse(eventTime)
  if (!Number.isFinite(startMs) || !Number.isFinite(eventMs) || eventMs < startMs) return '--'
  return `T+${formatMillis(eventMs - startMs)}`
}

function timingMarkerClass(event: ProvisionTiming) {
  const base = 'absolute left-0 top-[0.42rem] z-[1] h-[0.42rem] w-[0.42rem] border bg-panel'
  if (isFailure(event)) return `${base} border-error bg-error`
  if (event.result?.toLowerCase() === 'success') return `${base} border-brand bg-brand`
  return `${base} border-line-strong`
}

function timingResultClass(event: ProvisionTiming) {
  const result = event.result?.toLowerCase()
  const base = 'text-[0.72rem] font-medium'
  if (result === 'success') return `${base} text-ok`
  if (isFailure(event)) return `${base} text-error`
  return `${base} text-ink-soft`
}
