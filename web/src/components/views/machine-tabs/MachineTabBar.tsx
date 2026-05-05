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
    <nav className="flex gap-0 border-b border-line overflow-x-auto">
      {tabs.map(({ key, label }) => (
        <button
          key={key}
          className={clsx(
            'border-0 border-b-2 bg-transparent shadow-none py-[0.5rem] px-[0.85rem] text-[0.84rem] font-medium hover:transform-none! hover:shadow-none! hover:text-ink transition-colors',
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
