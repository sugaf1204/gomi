import { formatPowerControlMethod, powerStateClass, powerStateLabel } from '../../../lib/formatters'
import type { Machine } from '../../../types'

type Props = {
  machine: Machine
}

export function InfoTab({ machine }: Props) {
  return (
    <div className="pt-[0.65rem]">
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
    </div>
  )
}
