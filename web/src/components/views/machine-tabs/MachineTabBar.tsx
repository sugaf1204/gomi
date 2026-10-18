import type { MachineTab } from '../../../app-types'
import { ResourceSections } from '../../ui/ResourceSections'

const tabs: { key: MachineTab; label: string }[] = [
  { key: 'overview', label: 'Overview' },
  { key: 'deploy', label: 'Deploy' },
  { key: 'config', label: 'Config' },
]

type Props = { activeTab: MachineTab; onTabChange: (tab: MachineTab) => void }

export function MachineTabBar({ activeTab, onTabChange }: Props) {
  return <ResourceSections label="Machine sections" active={activeTab} items={tabs} onChange={onTabChange} />
}
