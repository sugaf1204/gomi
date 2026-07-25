import clsx from 'clsx'
import { formatDate } from '../../../lib/formatters'
import type { DHCPLease } from '../../../types'

export type DHCPLeaseTableProps = {
  dhcpLeases: DHCPLease[]
}

const COLUMNS = 'grid-cols-[150px_105px_minmax(0,1fr)_50px_130px]'
const HEADINGS = ['MAC', 'IP', 'HOSTNAME', 'PXE', 'LEASED AT']

export function DHCPLeaseTable({ dhcpLeases }: DHCPLeaseTableProps) {
  return (
    <section className="grid gap-2 content-start min-w-0">
      <div className="flex items-center gap-2">
        <span className="font-mono font-medium text-[10.5px] tracking-[0.1em] text-ink-soft">DHCP LEASES</span>
        <span className="font-mono text-[10.5px] text-ink-soft">{dhcpLeases.length}</span>
      </div>

      <div className="border border-line-strong bg-panel-2 overflow-x-auto">
        <div className={clsx('grid items-center min-w-[560px]', COLUMNS)}>
          {HEADINGS.map((heading) => (
            <span
              key={heading}
              className="p-2 font-mono font-medium text-[10.5px] tracking-[0.1em] text-ink-soft border-b border-line-strong"
            >
              {heading}
            </span>
          ))}

          {dhcpLeases.map((lease) => (
            <div key={lease.mac} className="contents">
              <span className="p-2 border-b border-line-soft font-mono text-[11.5px] truncate">{lease.mac}</span>
              <span className="p-2 border-b border-line-soft font-mono text-[11.5px] truncate">{lease.ip}</span>
              <span className="p-2 border-b border-line-soft font-mono text-[11.5px] truncate">
                {lease.hostname || '—'}
              </span>
              <span
                className={clsx(
                  'p-2 border-b border-line-soft font-mono text-[11.5px]',
                  lease.pxeClient ? 'text-warn' : 'text-ink-soft'
                )}
              >
                {lease.pxeClient ? 'yes' : 'no'}
              </span>
              <span className="p-2 border-b border-line-soft font-mono text-[11.5px] truncate">
                {formatDate(lease.leasedAt)}
              </span>
            </div>
          ))}
        </div>

        {dhcpLeases.length === 0 && (
          <p className="m-0 py-6 text-center font-mono text-[11.5px] text-ink-soft">No DHCP leases found</p>
        )}
      </div>
    </section>
  )
}
