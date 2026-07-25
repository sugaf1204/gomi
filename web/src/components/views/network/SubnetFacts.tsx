import type { ReactNode } from 'react'
import { formatDate } from '../../../lib/formatters'
import type { Subnet } from '../../../types'

export type SubnetFactsProps = {
  subnet: Subnet
  onEdit: () => void
  onDelete: () => void
}

type Fact = { key: string; value: ReactNode; mono?: boolean }

/** Renders a lease time in whichever unit keeps the number small. */
export function formatLeaseTime(seconds: number | undefined): string {
  if (!seconds || seconds === 0) return '1 hour (default)'
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${seconds / 60} min`
  if (seconds < 86400) return `${seconds / 3600} hours`
  return `${seconds / 86400} days`
}

function subnetFacts(subnet: Subnet): Fact[] {
  const { spec } = subnet
  const reserved = spec.reservedRanges?.length
    ? spec.reservedRanges.map((range) => `${range.start}–${range.end}`).join(', ')
    : '—'

  return [
    {
      key: 'PXE RANGE',
      value: spec.pxeAddressRange ? `${spec.pxeAddressRange.start}–${spec.pxeAddressRange.end}` : '—',
      mono: true,
    },
    { key: 'LEASE TIME', value: formatLeaseTime(spec.leaseTime), mono: true },
    { key: 'SEARCH', value: spec.dnsSearchDomains?.join(', ') || '—', mono: true },
    { key: 'DOMAIN', value: spec.domainName || '—', mono: true },
    { key: 'NTP', value: spec.ntpServers?.join(', ') || '—', mono: true },
    { key: 'RESERVED', value: reserved, mono: true },
    { key: 'UPDATED', value: subnet.updatedAt ? formatDate(subnet.updatedAt) : '—', mono: true },
  ]
}

export function SubnetFacts({ subnet, onEdit, onDelete }: SubnetFactsProps) {
  return (
    <section className="grid gap-2 content-start min-w-0">
      <div className="flex items-center gap-2">
        <span className="font-mono font-medium text-[10.5px] tracking-[0.1em] text-ink-soft">SUBNET</span>
        <span className="font-mono text-[10.5px] truncate">{subnet.name}</span>
      </div>

      <dl className="m-0 grid grid-cols-[104px_minmax(0,1fr)] gap-x-[10px] gap-y-[7px] content-start">
        {subnetFacts(subnet).map((fact) => (
          <div key={fact.key} className="contents">
            <dt className="font-mono text-[10.5px] text-ink-soft self-baseline">{fact.key}</dt>
            <dd className={`m-0 text-[12px] break-anywhere ${fact.mono ? 'font-mono' : ''}`}>{fact.value}</dd>
          </div>
        ))}
      </dl>

      <div className="flex gap-2 pt-1">
        <button className="py-[6px] px-[10px] text-[11.5px]" onClick={onEdit}>Edit</button>
        <button
          className="bg-danger-line border-danger text-white py-[6px] px-[10px] text-[11.5px]"
          onClick={onDelete}
        >
          Delete
        </button>
      </div>
    </section>
  )
}
