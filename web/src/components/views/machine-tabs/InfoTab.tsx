import { formatDate, formatPowerControlMethod, powerStateClass, powerStateLabel } from '../../../lib/formatters'
import type { Machine, ProvisionTiming } from '../../../types'

type Props = {
  machine: Machine
}

export function InfoTab({ machine }: Props) {
  const timings = machine.provision?.timings ?? []
  const visibleTimings = timings.slice(-24).reverse()

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
          <div className="overflow-auto max-h-[280px] border-0 border-t border-line">
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
        ) : (
          <p className="m-0 text-ink-soft text-[0.86rem]">No deploy timing has been reported for the current provisioning attempt.</p>
        )}
      </section>
    </div>
  )
}

function formatDuration(event: ProvisionTiming) {
  if (typeof event.durationMs === 'number' && Number.isFinite(event.durationMs) && event.durationMs > 0) {
    if (event.durationMs < 1000) return `${event.durationMs} ms`
    return `${(event.durationMs / 1000).toFixed(1)} s`
  }
  if (typeof event.monotonicSeconds === 'number' && Number.isFinite(event.monotonicSeconds) && event.monotonicSeconds > 0) {
    return `T+${event.monotonicSeconds.toFixed(2)} s`
  }
  return '-'
}

function formatTimingTime(event: ProvisionTiming) {
  return formatDate(event.finishedAt ?? event.timestamp ?? event.startedAt)
}
