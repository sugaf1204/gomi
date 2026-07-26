import { bandLabel, bandSize, type AddressSpace, type BandKind } from '../../../lib/address-space'

export type AddressSpaceRulerProps = {
  space: AddressSpace
}

/** Fill for each band kind. Reserved and pxe read as occupied, free as empty. */
const BAND_FILL: Record<BandKind, string> = {
  reserved: 'bg-neutral-bg',
  static: 'bg-brand-wash',
  free: 'bg-panel-2',
  pxe: 'bg-warn-bg',
}

const LEGEND_ORDER: BandKind[] = ['reserved', 'static', 'pxe', 'free']

export function AddressSpaceRuler({ space }: AddressSpaceRulerProps) {
  return (
    <section className="grid gap-2">
      <div className="flex items-center gap-2">
        <span className="font-mono font-medium text-[10.5px] tracking-[0.1em] text-ink-soft">ADDRESS SPACE</span>
        <span className="font-mono text-[10.5px] text-ink-soft">{space.totalAddresses} addresses</span>
      </div>

      <div>
        <div className="flex h-[30px] border border-line-strong bg-panel-2">
          {space.bands.map((band) => (
            <div
              key={band.startOffset}
              // flexGrow is the only proportional sizing available here: band
              // widths are data-derived, so they cannot be utility classes.
              style={{ flexGrow: bandSize(band), flexBasis: 0 }}
              className={BAND_FILL[band.kind]}
              title={`${bandLabel(band.kind)} · ${bandSize(band)} address${bandSize(band) === 1 ? '' : 'es'}`}
            />
          ))}
        </div>
        <TickRow space={space} />
      </div>

      <Legend />
    </section>
  )
}

// Narrow bands would stack their labels on top of each other, so a tick is
// dropped unless it clears the previously drawn one. The boundary is still
// visible in the bar above; only the redundant label goes.
const MIN_TICK_GAP_PCT = 6

function visibleTicks(space: AddressSpace): { offset: number; label: string; pct: number }[] {
  const kept: { offset: number; label: string; pct: number }[] = []
  for (const tick of space.ticks) {
    const pct = (tick.offset / space.totalAddresses) * 100
    const last = kept.at(-1)
    if (last && pct - last.pct < MIN_TICK_GAP_PCT) continue
    kept.push({ ...tick, pct })
  }
  return kept
}

/**
 * Tick labels sit under the boundary they mark. Each tick is absolutely placed
 * at its proportional offset so it tracks the band edge above it at any width.
 */
function TickRow({ space }: { space: AddressSpace }) {
  return (
    <div className="relative h-[14px]">
      {visibleTicks(space).map((tick) => (
        <span
          key={tick.offset}
          style={{ left: `${tick.pct}%`, transform: tick.pct > 92 ? 'translateX(-100%)' : 'none' }}
          className="absolute top-0 font-mono text-[10.5px] text-ink-soft whitespace-nowrap"
        >
          {tick.label}
        </span>
      ))}
    </div>
  )
}

function Legend() {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      {LEGEND_ORDER.map((kind) => (
        <span key={kind} className="flex items-center gap-[5px]">
          <span aria-hidden="true" className={`w-2 h-2 border border-line-strong ${BAND_FILL[kind]}`} />
          <span className="font-mono text-[10.5px] text-ink-soft">{bandLabel(kind)}</span>
        </span>
      ))}
    </div>
  )
}
