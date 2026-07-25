import type { ReactNode } from 'react'
import { formatPowerControlMethod, powerStateLabel } from '../../../lib/formatters'
import type { Machine, Subnet } from '../../../types'
import { HardwareFacts } from './HardwareFacts'

type Props = {
  machine: Machine
  subnets: Subnet[]
}

type Fact = { key: string; value: ReactNode; mono?: boolean }

/**
 * The default detail view: two fact columns replacing what used to be spread
 * across the Info, Detail and Network tabs.
 */
export function OverviewTab({ machine, subnets }: Props) {
  const subnet = subnets.find((candidate) => candidate.name === machine.subnetRef)

  const identity: Fact[] = [
    { key: 'HOSTNAME', value: machine.hostname, mono: true },
    { key: 'MAC', value: machine.mac, mono: true },
    { key: 'IP', value: machine.ip || '—', mono: true },
    { key: 'ASSIGNMENT', value: machine.ipAssignment === 'static' ? 'static' : 'dhcp' },
    { key: 'SUBNET', value: subnet ? `${subnet.name} · ${subnet.spec.cidr}` : machine.subnetRef || '—', mono: true },
    { key: 'DOMAIN', value: machine.network?.domain || '—', mono: true },
  ]

  const state: Fact[] = [
    { key: 'PHASE', value: machine.phase },
    { key: 'POWER', value: machine.powerState ? powerStateLabel(machine.powerState) : '—' },
    { key: 'POWER METHOD', value: formatPowerControlMethod(machine.power) },
    { key: 'OS IMAGE', value: `${machine.osPreset.family} ${machine.osPreset.version}`, mono: true },
    { key: 'TARGET DISK', value: machine.targetDisk || '—', mono: true },
    { key: 'UPDATED', value: machine.updatedAt || '—', mono: true },
  ]

  return (
    <div className="grid gap-[18px] pt-[14px]">
      <div className="grid grid-cols-2 gap-x-6 max-sm:grid-cols-1 border-t border-line pt-[14px]">
        <FactList facts={identity} />
        <FactList facts={state} />
      </div>

      {machine.lastError && (
        <div>
          <p className="m-0 mb-[6px] font-mono font-semibold text-[10px] tracking-[0.14em] text-ink-soft">LAST ERROR</p>
          <code className="block border border-error-line bg-error-bg text-error font-mono text-[11.5px] leading-[1.45] whitespace-pre-wrap break-anywhere py-[6px] px-2">
            {machine.lastError}
          </code>
        </div>
      )}

      <HardwareFacts machineName={machine.name} />
    </div>
  )
}

function FactList({ facts }: { facts: Fact[] }) {
  return (
    <dl className="m-0 grid grid-cols-[104px_minmax(0,1fr)] gap-x-[10px] gap-y-[7px] content-start">
      {facts.map((fact) => (
        <div key={fact.key} className="contents">
          <dt className="font-mono text-[10.5px] text-ink-soft self-baseline">{fact.key}</dt>
          <dd className={`m-0 text-[12px] break-anywhere ${fact.mono ? 'font-mono' : ''}`}>{fact.value}</dd>
        </div>
      ))}
    </dl>
  )
}
