import clsx from 'clsx'

type Props<T extends string> = {
  label: string
  active: T
  items: { key: T; label: string }[]
  onChange: (key: T) => void
}

export function ResourceSections<T extends string>({ label, active, items, onChange }: Props<T>) {
  return <nav aria-label={label} className="flex flex-wrap gap-x-[0.2rem] gap-y-[0.15rem] border-b border-line">
    {items.map((item) => <button key={item.key} aria-current={active === item.key ? 'page' : undefined}
      className={clsx(
        'shrink-0 border-0 border-b-2 bg-transparent shadow-none py-2 px-[13px] text-[12px] font-medium hover:transform-none! hover:shadow-none! hover:text-ink max-sm:px-[0.55rem] focus-visible:outline-2 focus-visible:outline-brand focus-visible:outline-offset-2',
        active === item.key ? 'border-b-brand text-brand' : 'border-b-transparent text-ink-soft hover:border-b-line-strong'
      )}
      onClick={() => onChange(item.key)}>{item.label}</button>)}
  </nav>
}
