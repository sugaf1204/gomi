import type { FormEvent } from 'react'
import type { ActivitySummary, ActivityType, MachineStats, MachineTab, Theme } from '../app-types'
import type { ActivityItem, GuardedAction, SubnetFormState } from '../app-types'
import type { WorkspaceContentProps } from '../components/layout/WorkspaceContent'
import type { AuditEvent, CloudInitTemplate, DHCPLease, Hypervisor, Machine, Me, OSImage, PowerConfig, SSHKey, Subnet, SystemInfo, VirtualMachine } from '../types'

type Params = {
  refreshAll: () => Promise<void>
  lastSyncedAt: string
  dataLoading: boolean
  machines: Machine[]
  audit: AuditEvent[]
  subnets: Subnet[]
  sshKeys: SSHKey[]
  hypervisors: Hypervisor[]
  virtualMachines: VirtualMachine[]
  upsertVirtualMachine: (virtualMachine: VirtualMachine) => void
  cloudInits: CloudInitTemplate[]
  osImages: OSImage[]
  dhcpLeases: DHCPLease[]
  systemInfo: SystemInfo | null
  me: Me | null
  theme: Theme
  setTheme: (theme: Theme) => void
  machineStats: MachineStats
  machineFilter: string
  setMachineFilter: (value: string) => void
  filteredMachines: Machine[]
  selectedMachine: string
  setSelectedMachine: (value: string) => void
  upsertMachine: (machine: Machine) => void
  selected: Machine | null
  openConfirm: (action: GuardedAction) => void
  saveMachineSettings: () => void | Promise<void>
  machineSettingsDirty: boolean
  machineSettingsSaving: boolean
  inlineEditField: 'power' | null
  setInlineEditField: (field: 'power' | null) => void
  machineSettingsPower: PowerConfig
  updateMachineSettingsPower: (value: PowerConfig) => void
  selectedVirtualMachineRoute: string
  setSelectedVirtualMachineRoute: (value: string) => void
  machineTab: MachineTab
  setMachineTab: (tab: MachineTab) => void
  activityTypeFilter: ActivityType
  setActivityTypeFilter: (value: ActivityType) => void
  activityMachineFilter: string
  setActivityMachineFilter: (value: string) => void
  filteredActivityItems: ActivityItem[]
  activitySummary: ActivitySummary
  subnetFormOpen: boolean
  toggleSubnetForm: () => void
  subnetForm: SubnetFormState
  updateSubnetForm: (field: keyof SubnetFormState, value: string) => void
  createSubnet: (e: FormEvent) => void | Promise<void>
  selectedSubnet: string
  setSelectedSubnet: (value: string) => void
  deleteSubnet: (name: string) => void | Promise<void>
  updateSubnet: (name: string, spec: Subnet['spec']) => void | Promise<void>
  selectedSubnetData: Subnet | null
}

type Result = Omit<WorkspaceContentProps, 'view'>

export function useWorkspaceContentProps({
  refreshAll,
  lastSyncedAt,
  dataLoading,
  machines,
  audit,
  subnets,
  sshKeys,
  hypervisors,
  virtualMachines,
  upsertVirtualMachine,
  cloudInits,
  osImages,
  dhcpLeases,
  systemInfo,
  me,
  theme,
  setTheme,
  machineStats,
  machineFilter,
  setMachineFilter,
  filteredMachines,
  selectedMachine,
  setSelectedMachine,
  upsertMachine,
  selected,
  openConfirm,
  saveMachineSettings,
  machineSettingsDirty,
  machineSettingsSaving,
  inlineEditField,
  setInlineEditField,
  machineSettingsPower,
  updateMachineSettingsPower,
  selectedVirtualMachineRoute,
  setSelectedVirtualMachineRoute,
  machineTab,
  setMachineTab,
  activityTypeFilter,
  setActivityTypeFilter,
  activityMachineFilter,
  setActivityMachineFilter,
  filteredActivityItems,
  activitySummary,
  subnetFormOpen,
  toggleSubnetForm,
  subnetForm,
  updateSubnetForm,
  createSubnet,
  selectedSubnet,
  setSelectedSubnet,
  deleteSubnet,
  updateSubnet,
  selectedSubnetData
}: Params): Result {
  return {
    header: {},
    overview: {
      lastSyncedAt,
      systemInfo,
      machineCount: machines.length,
      subnetCount: subnets.length,
      machineStats,
      hypervisorCount: hypervisors.length,
      vmCount: virtualMachines.length,
      vmStats: {
        running: virtualMachines.filter((vm) => vm.phase === 'Running').length,
        stopped: virtualMachines.filter((vm) => vm.phase === 'Stopped').length,
        error: virtualMachines.filter((vm) => vm.phase === 'Error').length
      }
    },
    machines: {
      machineFilter,
      onMachineFilterChange: setMachineFilter,
      filteredMachines,
      dataLoading,
      selectedMachineName: selectedMachine,
      onSelectMachine: setSelectedMachine,
      onMachineUpsert: upsertMachine,
      selectedMachine: selected,
      onOpenConfirm: openConfirm,
      onSaveMachineSettings: saveMachineSettings,
      machineSettingsDirty,
      machineSettingsSaving,
      inlineEditField,
      onInlineEditFieldChange: setInlineEditField,
      machineSettingsPower,
      onMachineSettingsPowerChange: updateMachineSettingsPower,
      auditEvents: audit,
      machineTab,
      onMachineTabChange: setMachineTab,
      subnets,
      onRefresh: refreshAll,
      hypervisors,
      osImages,
      cloudInits,
      sshKeys
    },
    hypervisors: {
      hypervisors,
      onRefresh: refreshAll
    },
    virtualMachines: {
      virtualMachines,
      hypervisors,
      osImages,
      cloudInits,
      sshKeys,
      subnets,
      dataLoading,
      onVirtualMachineUpsert: upsertVirtualMachine,
      routeSelectedVM: selectedVirtualMachineRoute,
      onRouteSelectedVMChange: setSelectedVirtualMachineRoute,
      onRefresh: refreshAll
    },
    activity: {
      activityTypeFilter,
      onActivityTypeChange: setActivityTypeFilter,
      activityMachineFilter,
      onActivityMachineChange: setActivityMachineFilter,
      machines,
      filteredItems: filteredActivityItems,
      summary: activitySummary
    },
    network: {
      subnetFormOpen,
      onToggleSubnetForm: toggleSubnetForm,
      subnetForm,
      onSubnetFormChange: updateSubnetForm,
      onCreateSubnet: createSubnet,
      subnets,
      selectedSubnet,
      onSelectSubnet: setSelectedSubnet,
      onDeleteSubnet: deleteSubnet,
      onUpdateSubnet: updateSubnet,
      selectedSubnetData
    },
    dhcpLeases: {
      dhcpLeases
    },
    cloudInit: {
      cloudInits,
      onRefresh: refreshAll
    },
    osImages: {
      osImages,
      onRefresh: refreshAll
    },
    users: {
      sshKeys,
      onRefresh: refreshAll
    },
    settings: {
      theme,
      onThemeChange: setTheme,
      user: me
    }
  }
}
