import { useState } from 'react'
import { api } from '../api'
import type { LoadState, View } from '../app-types'
import { useApiTokenEffect, useRefreshEffects } from './useAppEffects'
import type { AuditEvent, CloudInitTemplate, DHCPLease, Hypervisor, Machine, Me, OSImage, SSHKey, Subnet, SystemInfo, VirtualMachine } from '../types'

function upsertByName<T extends { name: string }>(items: T[], item: T) {
  const next = items.filter((current) => current.name !== item.name)
  next.push(item)
  next.sort((a, b) => a.name.localeCompare(b.name))
  return next
}

type Params = {
  token: string | null
  view: View
  selectedMachine: string
  activityMachineFilter: string
  toErrorMessage: (value: unknown) => string
}

export function useAppDataState({
  token,
  view,
  selectedMachine,
  activityMachineFilter,
  toErrorMessage
}: Params) {
  const [me, setMe] = useState<Me | null>(null)
  const [machines, setMachines] = useState<Machine[]>([])
  const [audit, setAudit] = useState<AuditEvent[]>([])
  const [subnets, setSubnets] = useState<Subnet[]>([])
  const [sshKeys, setSSHKeys] = useState<SSHKey[]>([])
  const [hypervisors, setHypervisors] = useState<Hypervisor[]>([])
  const [virtualMachines, setVirtualMachines] = useState<VirtualMachine[]>([])
  const [cloudInits, setCloudInits] = useState<CloudInitTemplate[]>([])
  const [osImages, setOSImages] = useState<OSImage[]>([])
  const [dhcpLeases, setDHCPLeases] = useState<DHCPLease[]>([])
  const [systemInfo, setSystemInfo] = useState<SystemInfo | null>(null)
  const [error, setError] = useState('')
  const [state, setState] = useState<LoadState>('idle')
  const [lastSyncedAt, setLastSyncedAt] = useState<string>('')
  const [hasLoadedOnce, setHasLoadedOnce] = useState(false)

  useApiTokenEffect(token)

  async function refreshAll() {
    if (!hasLoadedOnce) {
      setState('loading')
    }
    setError('')
    try {
      const [meData, machineData, subnetData, sshKeyData, hypervisorData, vmData, cloudInitData, osImageData, dhcpLeaseData, systemInfoData] = await Promise.allSettled([
        api.me(),
        api.listMachines(),
        api.listSubnets(),
        api.listSSHKeys(),
        api.listHypervisors(),
        api.listVirtualMachines(),
        api.listCloudInitTemplates(),
        api.listOSImages(),
        api.listDHCPLeases(),
        api.systemInfo()
      ])
      if (meData.status === 'rejected') throw meData.reason
      if (machineData.status === 'rejected') throw machineData.reason

      setMe(meData.value)
      setMachines(machineData.value.items ?? [])

      if (subnetData.status === 'fulfilled') {
        setSubnets(subnetData.value.items ?? [])
      } else {
        setSubnets([])
      }
      if (sshKeyData.status === 'fulfilled') {
        setSSHKeys(sshKeyData.value.items ?? [])
      } else {
        setSSHKeys([])
      }
      if (hypervisorData.status === 'fulfilled') {
        setHypervisors(hypervisorData.value.items ?? [])
      } else {
        setHypervisors([])
      }
      if (vmData.status === 'fulfilled') {
        setVirtualMachines(vmData.value.items ?? [])
      } else {
        setVirtualMachines([])
      }
      if (cloudInitData.status === 'fulfilled') {
        setCloudInits(cloudInitData.value.items ?? [])
      } else {
        setCloudInits([])
      }
      if (osImageData.status === 'fulfilled') {
        setOSImages(osImageData.value.items ?? [])
      } else {
        setOSImages([])
      }
      if (dhcpLeaseData.status === 'fulfilled') {
        setDHCPLeases(dhcpLeaseData.value.items ?? [])
      } else {
        setDHCPLeases([])
      }
      if (systemInfoData.status === 'fulfilled') {
        setSystemInfo(systemInfoData.value)
      } else {
        setSystemInfo(null)
      }

      setLastSyncedAt(new Date().toISOString())
      setHasLoadedOnce(true)
      setState('idle')
    } catch (e) {
      setHasLoadedOnce(true)
      setState('error')
      setError(toErrorMessage(e))
    }
  }

  async function refreshAudit(machineName?: string) {
    if (!token) return
    try {
      const response = await api.listAudit(machineName)
      setAudit(response.items ?? [])
    } catch {
      setAudit([])
    }
  }

  useRefreshEffects({
    token,
    view,
    selectedMachine,
    activityMachineFilter,
    refreshAll,
    refreshAudit,
    setAudit
  })

  function resetData() {
    setMe(null)
    setMachines([])
    setAudit([])
    setSubnets([])
    setSSHKeys([])
    setHypervisors([])
    setVirtualMachines([])
    setCloudInits([])
    setOSImages([])
    setDHCPLeases([])
    setSystemInfo(null)
    setError('')
    setState('idle')
    setLastSyncedAt('')
    setHasLoadedOnce(false)
  }

  function upsertMachine(machine: Machine) {
    setMachines((current) => upsertByName(current, machine))
  }

  function upsertVirtualMachine(virtualMachine: VirtualMachine) {
    setVirtualMachines((current) => upsertByName(current, virtualMachine))
  }

  return {
    me,
    machines,
    audit,
    subnets,
    sshKeys,
    hypervisors,
    virtualMachines,
    cloudInits,
    osImages,
    dhcpLeases,
    systemInfo,
    error,
    setError,
    state,
    hasLoadedOnce,
    lastSyncedAt,
    refreshAll,
    refreshAudit,
    upsertMachine,
    upsertVirtualMachine,
    resetData
  }
}
