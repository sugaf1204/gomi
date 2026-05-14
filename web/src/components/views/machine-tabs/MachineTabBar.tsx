import clsx from 'clsx'
import type { MachineTab } from '../../../app-types'

const tabs: { key: MachineTab; label: string }[] = [
  { key: 'info', label: 'Info' },
  { key: 'detail', label: 'Detail' },
  { key: 'network', label: 'Network' },
  { key: 'console', label: 'Console' },
  { key: 'activity', label: 'Activity' },
  { key: 'configuration', label: 'Configuration' },
]

type Props = {
  activeTab: MachineTab
  onTabChange: (tab: MachineTab) => void
}

export function MachineTabBar({ activeTab, onTabChange }: Props) {
  return (
    <nav aria-label="Machine sections" className="flex flex-wrap gap-x-[0.2rem] gap-y-[0.15rem] border-b border-line">
      {tabs.map(({ key, label }) => (
        <button
          key={key}
          className={clsx(
            'shrink-0 border-0 border-b-2 bg-transparent shadow-none py-[0.5rem] px-[0.78rem] text-[0.84rem] font-medium hover:transform-none! hover:shadow-none! hover:text-ink transition-colors max-sm:px-[0.55rem]',
            activeTab === key
              ? 'border-b-brand text-brand'
              : 'border-b-transparent text-ink-soft hover:border-b-line-strong'
          )}
          onClick={() => onTabChange(key)}
        >
          {label}
        </button>
      ))}
    </nav>
  )
}
