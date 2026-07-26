import type { View } from '../../app-types'
import { ActivityView, type ActivityViewProps } from '../views/ActivityView'
import { CloudInitView, type CloudInitViewProps } from '../views/CloudInitView'
import { DHCPLeasesView, type DHCPLeasesViewProps } from '../views/DHCPLeasesView'
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
  dhcpLeases: DHCPLeasesViewProps
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
  dhcpLeases,
  dnsRecords,
  cloudInit,
  osImages,
  users,
  settings
}: WorkspaceContentProps) {
  return (
    <section className="workspace-shell h-screen grid grid-rows-[auto_minmax(0,1fr)] max-sm:h-auto">
      <WorkspaceHeader {...header} />

      <div className="min-h-0 overflow-y-auto p-[20px_22px] max-sm:p-[0.8rem]">
        {view === 'overview' && <OverviewView {...overview} />}
        {view === 'machines' && <MachinesView {...machines} />}
        {view === 'hypervisors' && <HypervisorsView {...hypervisors} />}
        {view === 'virtual-machines' && <VirtualMachinesView {...virtualMachines} />}
        {view === 'activity' && <ActivityView {...activity} />}
        {view === 'network' && <NetworkView {...network} />}
        {view === 'dhcp-leases' && <DHCPLeasesView {...dhcpLeases} />}
        {view === 'dns-records' && <DNSRecordsView {...dnsRecords} />}
        {view === 'cloud-init' && <CloudInitView {...cloudInit} />}
        {view === 'os-images' && <OSImagesView {...osImages} />}
        {view === 'users' && <UsersView {...users} />}
        {view === 'settings' && <SettingsView {...settings} />}
      </div>
    </section>
  )
}
