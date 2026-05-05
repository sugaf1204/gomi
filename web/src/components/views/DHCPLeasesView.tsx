import { formatDate } from '../../lib/formatters'
import type { DHCPLease } from '../../types'

export type DHCPLeasesViewProps = {
  dhcpLeases: DHCPLease[]
}

export function DHCPLeasesView({ dhcpLeases }: DHCPLeasesViewProps) {
  return (
    <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.65rem] self-start">
      <h2 className="text-[1.4rem]">DHCP Leases</h2>

      <div className="overflow-auto max-h-[65vh]">
        <table>
          <thead>
            <tr>
              <th>MAC</th>
              <th>IP</th>
              <th>Hostname</th>
              <th>PXE</th>
              <th>Leased At</th>
            </tr>
          </thead>
          <tbody>
            {dhcpLeases.map((lease) => (
              <tr key={lease.mac}>
                <td><code>{lease.mac}</code></td>
                <td><code>{lease.ip}</code></td>
                <td>{lease.hostname || '-'}</td>
                <td>{lease.pxeClient ? 'Yes' : 'No'}</td>
                <td>{formatDate(lease.leasedAt)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {dhcpLeases.length === 0 && <p className="m-0 text-ink-soft">No DHCP leases found</p>}
      </div>
    </section>
  )
}
