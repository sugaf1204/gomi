import clsx from 'clsx'
import type { View } from '../../app-types'
import { ActivityView, type ActivityViewProps } from '../views/ActivityView'
import { CloudInitView, type CloudInitViewProps } from '../views/CloudInitView'
import { DNSRecordsView, type DNSRecordsViewProps } from '../views/DNSRecordsView'
import { HypervisorsView, type HypervisorsViewProps } from '../views/HypervisorsView'
import { MachinesView, type MachinesViewProps } from '../views/MachinesView'
import { NetworkView, type NetworkViewProps } from '../views/NetworkView'
import { OSImagesView, type OSImagesViewProps } from '../views/OSImagesView'
import { OverviewView, type OverviewViewProps } from '../views/OverviewView'
import { SettingsView, type SettingsViewProps } from '../views/SettingsView'
import { UsersView, type UsersViewProps } from '../views/UsersView'
import { VirtualMachinesView, type VirtualMachinesViewProps } from '../views/VirtualMachinesView'
import { WorkspaceHeader, type WorkspaceHeaderProps } from './WorkspaceHeader'

export type WorkspaceContentProps = {
  view: View
  header: WorkspaceHeaderProps
  overview: OverviewViewProps
  machines: MachinesViewProps
  hypervisors: HypervisorsViewProps
  virtualMachines: VirtualMachinesViewProps
  activity: ActivityViewProps
  network: NetworkViewProps
  dnsRecords: DNSRecordsViewProps
  cloudInit: CloudInitViewProps
  osImages: OSImagesViewProps
  users: UsersViewProps
  settings: SettingsViewProps
}

export function WorkspaceContent({
  view,
  header,
  overview,
  machines,
  hypervisors,
  virtualMachines,
  activity,
  network,
  dnsRecords,
  cloudInit,
  osImages,
  users,
  settings
}: WorkspaceContentProps) {
  // Split views own their full height and scroll each column independently, so
  // the shell must not add padding or a scroll container around them.
  const selfLayout = view === 'machines' || view === 'virtual-machines'

  return (
    <section className="workspace-shell h-screen grid grid-rows-[auto_minmax(0,1fr)] max-sm:h-auto">
      <WorkspaceHeader {...header} />

      <div className={clsx('min-h-0', selfLayout ? 'overflow-hidden' : 'overflow-y-auto p-[20px_22px] max-sm:p-[0.8rem]')}>
        {view === 'overview' && <OverviewView {...overview} />}
        {view === 'machines' && <MachinesView {...machines} />}
        {view === 'hypervisors' && <HypervisorsView {...hypervisors} />}
        {view === 'virtual-machines' && <VirtualMachinesView {...virtualMachines} />}
        {view === 'activity' && <ActivityView {...activity} />}
        {view === 'network' && <NetworkView {...network} />}
        {view === 'dns-records' && <DNSRecordsView {...dnsRecords} />}
        {view === 'cloud-init' && <CloudInitView {...cloudInit} />}
        {view === 'os-images' && <OSImagesView {...osImages} />}
        {view === 'users' && <UsersView {...users} />}
        {view === 'settings' && <SettingsView {...settings} />}
      </div>
    </section>
  )
}
