import { formatDate, formatPowerControlMethod, powerStateClass, powerStateLabel } from '../../../lib/formatters'
import type { Machine, ProvisionTiming } from '../../../types'

type Props = {
  machine: Machine
}

export function InfoTab({ machine }: Props) {
  const timings = machine.provision?.timings ?? []
  const visibleTimings = timings.slice(-24).reverse()
  const summary = buildTimingSummary(machine, timings)

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
            <div className="overflow-auto max-h-[320px] border-0 border-t border-line">
              <table className="w-full border-collapse text-[0.82rem]">
                <thead>
                  <tr className="text-left text-ink-soft">
                    <th className="font-medium py-[0.42rem] pr-[0.65rem]">Stage</th>
                    <th className="font-medium py-[0.42rem] pr-[0.65rem]">Source</th>
                    <th className="font-medium py-[0.42rem] pr-[0.65rem]">Duration</th>
                    <th className="font-medium py-[0.42rem] pr-[0.65rem]">Result</th>
                    <th className="font-medium py-[0.42rem]">Time</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleTimings.map((event, index) => (
                    <tr key={`${event.source ?? 'unknown'}-${event.name}-${event.timestamp ?? event.finishedAt ?? index}`} className="border-0 border-t border-line align-top">
                      <td className="py-[0.42rem] pr-[0.65rem] min-w-[180px]">
                        <span className="font-medium">{event.name}</span>
                        {event.message && <span className="block text-ink-soft text-[0.76rem] leading-[1.35]">{event.message}</span>}
                      </td>
                      <td className="py-[0.42rem] pr-[0.65rem]">{event.source || '-'}</td>
                      <td className="py-[0.42rem] pr-[0.65rem] tabular-nums">{formatDuration(event)}</td>
                      <td className="py-[0.42rem] pr-[0.65rem]">{event.result || event.eventType || '-'}</td>
                      <td className="py-[0.42rem] tabular-nums whitespace-nowrap">{formatTimingTime(event)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
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
  const partitioning = latestTiming(timings, 'cmd-install/stage-partitioning')
  const extract = latestTiming(timings, 'cmd-install/stage-extract')
  const attemptDuration = durationBetween(machine.provision?.startedAt, machine.provision?.completedAt ?? machine.provision?.finishedAt)

  return [
    { label: 'Attempt', value: attemptDuration ?? '-', detail: machine.provision?.trigger || undefined },
    { label: 'Inventory Store', value: inventoryStore ? formatDuration(inventoryStore) : '-', detail: inventoryStore?.name },
    { label: 'Image Transfer', value: imageTransfer ? formatDuration(imageTransfer) : '-', detail: imageTransfer?.message },
    { label: 'Curtin Install', value: curtinInstall ? formatDuration(curtinInstall) : '-', detail: curtinInstall?.result },
    { label: 'Partitioning', value: partitioning ? formatDuration(partitioning) : '-', detail: partitioning?.result },
    { label: 'Extract', value: extract ? formatDuration(extract) : '-', detail: extract?.result },
    { label: 'Slowest', value: slowest ? formatDuration(slowest) : '-', detail: slowest ? slowest.name : undefined },
  ]
}

function latestTiming(timings: ProvisionTiming[], name: string) {
  for (let index = timings.length - 1; index >= 0; index -= 1) {
    if (timings[index].name === name) return timings[index]
  }
  return undefined
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
  if (typeof event.monotonicSeconds === 'number' && Number.isFinite(event.monotonicSeconds) && event.monotonicSeconds > 0) {
    return `T+${event.monotonicSeconds.toFixed(2)} s`
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
