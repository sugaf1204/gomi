export type FleetSlice = {
  key: string
  count: number
  label: string
  /** Token utilities for the fill, its border, and the inline count text. */
  fill: string
  border: string
  text: string
}

type Props = {
  heading: string
  total: number
  summary: string
  slices: FleetSlice[]
}

/**
 * One horizontal bar per fleet, partitioned by state. Widths carry the
 * proportion; nothing animates. A slice with no members is omitted rather
 * than rendered as a hairline.
 */
export function FleetBar({ heading, total, summary, slices }: Props) {
  const present = slices.filter((slice) => slice.count > 0)
  const sum = present.reduce((acc, slice) => acc + slice.count, 0)

  return (
    <section>
      <div className="flex items-baseline gap-3 mb-2">
        <p className="m-0 font-mono font-semibold text-[10px] tracking-[0.16em] text-ink-soft">{heading}</p>
        <p className="m-0 font-mono font-medium text-[12px]">{total}</p>
        <p className="m-0 ml-auto font-mono text-[10.5px] text-ink-soft truncate">{summary}</p>
      </div>

      <div className="flex h-[34px] border border-line-strong bg-panel-2 overflow-hidden">
        {present.map((slice, index) => (
          <div
            key={slice.key}
            className={`flex items-center justify-center min-w-0 ${slice.fill} ${index < present.length - 1 ? `border-r ${slice.border}` : ''}`}
            style={{ flexGrow: slice.count, flexBasis: 0 }}
          >
            <span className={`font-mono font-medium text-[10.5px] truncate px-1 ${slice.text}`}>
              {slice.count} {slice.label}
            </span>
          </div>
        ))}
        {sum === 0 && (
          <div className="flex items-center px-2">
            <span className="font-mono text-[10.5px] text-ink-soft">none</span>
          </div>
        )}
      </div>
    </section>
  )
}
