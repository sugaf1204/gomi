import { useMemo } from 'react'
import type { ActivityItem, ActivitySummary, ActivityType, MachineSettingsDraft, MachineStats } from '../app-types'
import type { AuditEvent, Machine, Subnet } from '../types'

type Params = {
  machines: Machine[]
  selectedMachine: string
  machineSettingsDraft: MachineSettingsDraft
  subnets: Subnet[]
  selectedSubnet: string
  audit: AuditEvent[]
  activityTypeFilter: ActivityType
  activityMachineFilter: string
  machineFilter: string
}

export function useAppDerivedData({
  machines,
  selectedMachine,
  machineSettingsDraft,
  subnets,
  selectedSubnet,
  audit,
  activityTypeFilter,
  activityMachineFilter,
  machineFilter
}: Params) {
  const selected = useMemo(() => machines.find((machine) => machine.name === selectedMachine) ?? null, [machines, selectedMachine])

  const selectedSubnetData = useMemo(() => subnets.find((subnet) => subnet.name === selectedSubnet) ?? null, [subnets, selectedSubnet])

  const activityItems = useMemo(() => {
    const items: ActivityItem[] = []

    for (const event of audit) {
      items.push({
        id: `audit-${event.id}`,
        type: 'audit',
        machine: event.machine,
        action: event.action,
        actor: event.actor,
        result: event.result,
        message: event.message,
        timestamp: event.createdAt
      })
    }

    items.sort((a, b) => (b.timestamp || '').localeCompare(a.timestamp || ''))
    return items
  }, [audit])

  const filteredActivityItems = useMemo(() => {
    let items = activityItems
    if (activityTypeFilter !== 'all') {
      items = items.filter((item) => item.type === activityTypeFilter)
    }
    if (activityMachineFilter) {
      items = items.filter((item) => item.machine === activityMachineFilter)
    }
    return items
  }, [activityItems, activityTypeFilter, activityMachineFilter])

  const activitySummary = useMemo<ActivitySummary>(() => {
    let auditCount = 0
    let success = 0
    let failure = 0

    for (const item of activityItems) {
      auditCount++
      const result = item.result.toLowerCase()
      if (result === 'success' || result === 'succeeded' || result === 'ready') success++
      else if (result === 'failed' || result === 'failure' || result === 'error') failure++
    }

    return { total: activityItems.length, auditCount, success, failure }
  }, [activityItems])

  const machineSettingsDirty = useMemo(() => {
    if (!selected) return false
    return JSON.stringify(selected.power) !== JSON.stringify(machineSettingsDraft.power)
  }, [selected, machineSettingsDraft.power])

  const machineStats = useMemo<MachineStats>(() => {
    let ready = 0
    let provisioning = 0
    let attention = 0

    for (const machine of machines) {
      const phase = machine.phase.toLowerCase()
      if (phase === 'ready') {
        ready++
      } else if (phase === 'provisioning') {
        provisioning++
      } else {
        attention++
      }
    }

    return { ready, provisioning, attention }
  }, [machines])

  const filteredMachines = useMemo(() => {
    const query = machineFilter.trim().toLowerCase()
    if (!query) return machines
    return machines.filter((machine) => {
      return (
        machine.name.toLowerCase().includes(query) ||
        machine.hostname.toLowerCase().includes(query) ||
        machine.mac.toLowerCase().includes(query)
      )
    })
  }, [machines, machineFilter])

  return {
    selected,
    selectedSubnetData,
    filteredActivityItems,
    activitySummary,
    machineSettingsDirty,
    machineStats,
    filteredMachines
  }
}
