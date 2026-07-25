import clsx from 'clsx'
import type { Hypervisor, VirtualMachine } from '../../../types'

/**
 * One resource row: how much of the host's capacity this VM claims. `share` is
 * null when the host never reported a capacity for that dimension, which keeps
 * the track empty and the label honest instead of inventing a denominator.
 */
type ShareRow = {
  label: string
  value: string
  total: string
  share: number | null
  tone: 'bg-brand' | 'bg-brand/60'
}

const UNKNOWN_TOTAL = 'host capacity unknown'

/** Clamped VM-over-host ratio; null whenever the host total is missing or zero. */
function shareOf(used: number, total?: number): number | null {
  if (typeof total !== 'number' || !Number.isFinite(total) || total <= 0) return null
  return Math.min(1, Math.max(0, used / total))
}

function gb(memoryMB: number): string {
  return `${Math.round(memoryMB / 1024)}g`
}

function buildRows(vm: VirtualMachine, hypervisor?: Hypervisor): ShareRow[] {
  const capacity = hypervisor?.capacity
  return [
    {
      label: 'vCPU',
      value: `${vm.resources.cpuCores}c`,
      total: capacity ? `of ${capacity.cpuCores}c` : UNKNOWN_TOTAL,
      share: shareOf(vm.resources.cpuCores, capacity?.cpuCores),
      tone: 'bg-brand',
    },
    {
      label: 'Memory',
      value: gb(vm.resources.memoryMB),
      total: capacity ? `of ${gb(capacity.memoryMB)}` : UNKNOWN_TOTAL,
      share: shareOf(vm.resources.memoryMB, capacity?.memoryMB),
      tone: 'bg-brand',
    },
    {
      label: 'Disk',
      value: `${vm.resources.diskGB}g`,
      total: capacity?.storageGB ? `of ${capacity.storageGB}g` : UNKNOWN_TOTAL,
      share: shareOf(vm.resources.diskGB, capacity?.storageGB),
      tone: 'bg-brand/60',
    },
  ]
}

export type VMResourceShareCardProps = {
  vm: VirtualMachine
  hypervisor?: Hypervisor
}

export function VMResourceShareCard({ vm, hypervisor }: VMResourceShareCardProps) {
  const hostLabel = (hypervisor?.name ?? vm.hypervisorRef ?? '').toUpperCase()
  return (
    <article className="border border-line bg-panel p-[14px_16px]">
      <h3 className="m-0 mb-[10px] font-mono font-semibold text-[10px] uppercase tracking-[0.14em] text-ink-soft">
        Resources{hostLabel ? ` · Share of ${hostLabel}` : ''}
      </h3>
      <div className="grid gap-[10px]">
        {buildRows(vm, hypervisor).map((row) => (
          <ShareMeter key={row.label} row={row} />
        ))}
      </div>
    </article>
  )
}

function ShareMeter({ row }: { row: ShareRow }) {
  return (
    <div>
      <p className="m-0 flex items-baseline gap-2 font-mono text-[10.5px]">
        <span>{row.label}</span>
        <span className="ml-auto font-medium">{row.value}</span>
        <span className="text-ink-soft">{row.total}</span>
      </p>
      <div
        role="img"
        aria-label={
          row.share === null
            ? `${row.label} ${row.value}, ${UNKNOWN_TOTAL}`
            : `${row.label} ${row.value} ${row.total}, ${Math.round(row.share * 100)}% of host`
        }
        className="mt-[4px] h-[8px] bg-panel-3 border border-line-soft"
      >
        {row.share !== null && (
          <span
            aria-hidden="true"
            className={clsx('block h-full', row.tone)}
            style={{ width: `${row.share * 100}%` }}
          />
        )}
      </div>
    </div>
  )
}
