import clsx from 'clsx'
import type { Subnet } from '../../../types'

export type SubnetTableProps = {
  subnets: Subnet[]
  selectedSubnet: string
  onSelectSubnet: (name: string) => void
  onCreateSubnet: () => void
}

const COLUMNS = 'grid-cols-[150px_130px_110px_60px_100px_minmax(0,1fr)]'
const HEADINGS = ['NAME', 'CIDR', 'GATEWAY', 'VLAN', 'PXE IFACE', 'DNS']

export function SubnetTable({ subnets, selectedSubnet, onSelectSubnet, onCreateSubnet }: SubnetTableProps) {
  return (
    <section className="grid gap-2">
      <div className="flex items-center gap-2">
        <span className="font-mono font-medium text-[10.5px] tracking-[0.1em] text-ink-soft">SUBNETS</span>
        <span className="font-mono text-[10.5px] text-ink-soft">{subnets.length}</span>
        <button className="ml-auto shrink-0 py-[6px] px-[10px] text-[11.5px]" onClick={onCreateSubnet}>
          Create Subnet
        </button>
      </div>

      <div className="border border-line-strong bg-panel-2">
        <div className={clsx('grid items-center', COLUMNS)}>
          {HEADINGS.map((heading) => (
            <span
              key={heading}
              className="p-2 font-mono font-medium text-[10.5px] tracking-[0.1em] text-ink-soft border-b border-line-strong"
            >
              {heading}
            </span>
          ))}
        </div>

        {subnets.map((subnet) => (
          <SubnetRow
            key={subnet.name}
            subnet={subnet}
            selected={selectedSubnet === subnet.name}
            onSelect={onSelectSubnet}
          />
        ))}

        {subnets.length === 0 && (
          <p className="m-0 py-6 text-center font-mono text-[11.5px] text-ink-soft">No subnets found</p>
        )}
      </div>
    </section>
  )
}

function SubnetRow({
  subnet,
  selected,
  onSelect,
}: {
  subnet: Subnet
  selected: boolean
  onSelect: (name: string) => void
}) {
  // The selected row carries a 3px brand edge; unselected rows keep an
  // equivalent transparent border so the grid columns never shift.
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={() => onSelect(subnet.name)}
      className={clsx(
        'w-full text-left grid items-center border-0 border-b border-line-soft p-0 shadow-none',
        'border-l-[3px] hover:transform-none! hover:shadow-none!',
        COLUMNS,
        selected ? 'bg-panel border-l-brand' : 'bg-transparent border-l-transparent'
      )}
    >
      <span className="p-2 font-mono font-medium text-[11.5px] truncate">{subnet.name}</span>
      <span className="p-2 font-mono text-[11.5px] truncate">{subnet.spec.cidr}</span>
      <span className="p-2 font-mono text-[11.5px] truncate">{subnet.spec.defaultGateway || '—'}</span>
      <span className="p-2 font-mono text-[11.5px] truncate">{subnet.spec.vlanId ?? '—'}</span>
      <span className="p-2 font-mono text-[11.5px] truncate">{subnet.spec.pxeInterface || '—'}</span>
      <span className="p-2 font-mono text-[11.5px] truncate">{subnet.spec.dnsServers?.join(', ') || '—'}</span>
    </button>
  )
}
