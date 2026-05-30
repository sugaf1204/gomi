import type { AuditEvent, BootEnvironmentStatus, CloudInitTemplate, DHCPLease, DNSRecord, HardwareInfo, Hypervisor, Machine, Me, OSImage, PowerConfig, SSHKey, Subnet, SystemInfo, VirtualMachine } from './types'

const API_BASE = import.meta.env.VITE_API_BASE ?? `${window.location.origin}/api/v1`

type ListResponse<K extends string, T> = Record<K, T[]> & {
  nextPageToken?: string
  totalSize?: number
}

type MachineApi = Omit<Machine, 'name' | 'osPreset'> & {
  name: string
  machineId?: string
  cloudInitRef?: string
  osPreset: Machine['osPreset']
}

type VirtualMachineApi = Omit<VirtualMachine, 'name' | 'installCfg'> & {
  name: string
  virtualMachineId?: string
  installConfig?: VirtualMachine['installCfg']
}

type NamedApi<T, ID extends string> = T & {
  name: string
} & Record<ID, string | undefined>

type MachineSpecPayload = {
  hostname?: string
  mac?: string
  arch?: string
  firmware?: string
  power?: PowerConfig
  osPreset?: {
    imageRef?: string
    family?: Machine['osPreset']['family']
    version?: string
  }
  targetDisk?: string
  network?: {
    domain?: string
  }
  cloudInitRef?: string
  cloudInitRefs?: string[]
  subnetRef?: string
  ipAssignment?: string
  ip?: string
  role?: Machine['role']
  bridgeName?: string
  sshKeyRefs?: string[]
  loginUser?: Machine['loginUser']
}

type MachineRedeployPayload = MachineSpecPayload

type VMRedeployPayload = {
  hypervisorRef?: string
  resources?: VirtualMachine['resources']
  osImageRef?: string
  cloudInitRefs?: string[]
  cloudInitRef?: string
  installCfg?: VirtualMachine['installCfg']
  advancedOptions?: VirtualMachine['advancedOptions']
  network?: VirtualMachine['network']
  subnetRef?: string
  domain?: string
  ipAssignment?: string
  ip?: string
  sshKeyRefs?: string[]
  loginUser?: VirtualMachine['loginUser']
}

type VMApiSpecPayload = Omit<VMRedeployPayload, 'installCfg'> & {
  installConfig?: VirtualMachine['installCfg']
}

function resourceName(collection: string, id?: string) {
  const value = id?.trim() ?? ''
  if (!value) return ''
  return value.startsWith(`${collection}/`) ? value : `${collection}/${value}`
}

function resourceId(collection: string, name?: string) {
  const value = name?.trim() ?? ''
  if (!value) return ''
  return value.startsWith(`${collection}/`) ? value.slice(collection.length + 1) : value
}

function resourceIds(collection: string, names?: string[]) {
  return names?.map((name) => resourceId(collection, name)).filter(Boolean)
}

function resourceNames(collection: string, ids?: string[]) {
  return ids?.map((id) => resourceName(collection, id)).filter(Boolean)
}

function fromApiMachine(input: MachineApi): Machine {
  return {
    ...input,
    name: input.machineId || resourceId('machines', input.name),
    osPreset: {
      ...input.osPreset,
      imageRef: resourceId('osImages', input.osPreset?.imageRef)
    },
    cloudInitRefs: resourceIds('cloudInitTemplates', input.cloudInitRefs),
    subnetRef: resourceId('subnets', input.subnetRef),
    sshKeyRefs: resourceIds('sshKeys', input.sshKeyRefs),
    lastDeployedCloudInitRef: resourceId('cloudInitTemplates', input.lastDeployedCloudInitRef)
  }
}

function toApiMachineSpec<T extends MachineSpecPayload>(input: T): T {
  return {
    ...input,
    osPreset: input.osPreset
      ? { ...input.osPreset, imageRef: resourceName('osImages', input.osPreset.imageRef) }
      : input.osPreset,
    cloudInitRef: resourceName('cloudInitTemplates', input.cloudInitRef),
    cloudInitRefs: resourceNames('cloudInitTemplates', input.cloudInitRefs),
    subnetRef: resourceName('subnets', input.subnetRef),
    sshKeyRefs: resourceNames('sshKeys', input.sshKeyRefs)
  }
}

function fromApiVirtualMachine(input: VirtualMachineApi): VirtualMachine {
  const { installConfig, ...rest } = input
  return {
    ...rest,
    name: input.virtualMachineId || resourceId('virtualMachines', input.name),
    hypervisorRef: resourceId('hypervisors', input.hypervisorRef),
    osImageRef: resourceId('osImages', input.osImageRef),
    cloudInitRef: resourceId('cloudInitTemplates', input.cloudInitRef),
    cloudInitRefs: resourceIds('cloudInitTemplates', input.cloudInitRefs),
    subnetRef: resourceId('subnets', input.subnetRef),
    sshKeyRefs: resourceIds('sshKeys', input.sshKeyRefs),
    lastDeployedCloudInitRef: resourceId('cloudInitTemplates', input.lastDeployedCloudInitRef),
    installCfg: installConfig
  }
}

function toApiVirtualMachineSpec(input: VMRedeployPayload): VMApiSpecPayload {
  const { installCfg, ...rest } = input
  return {
    ...rest,
    hypervisorRef: resourceName('hypervisors', rest.hypervisorRef),
    osImageRef: resourceName('osImages', rest.osImageRef),
    cloudInitRef: resourceName('cloudInitTemplates', rest.cloudInitRef),
    cloudInitRefs: resourceNames('cloudInitTemplates', rest.cloudInitRefs),
    subnetRef: resourceName('subnets', rest.subnetRef),
    sshKeyRefs: resourceNames('sshKeys', rest.sshKeyRefs),
    installConfig: installCfg
  }
}

function fromApiNamed<T extends { name: string }, ID extends string>(collection: string, idField: ID, input: NamedApi<T, ID>): T {
  return {
    ...input,
    name: input[idField] || resourceId(collection, input.name)
  }
}

function fromApiHypervisor(input: NamedApi<Hypervisor, 'hypervisorId'>): Hypervisor {
  return {
    ...fromApiNamed('hypervisors', 'hypervisorId', input),
    machineRef: resourceId('machines', input.machineRef),
    connection: {
      ...input.connection,
      keyRef: resourceId('sshKeys', input.connection.keyRef)
    }
  }
}

function fromApiOSImage(input: NamedApi<OSImage, 'osImageId'>): OSImage {
  return fromApiNamed('osImages', 'osImageId', input)
}

class ApiClient {
  private token: string | null = null

  setToken(token: string | null) {
    this.token = token
  }

  private async request<T>(path: string, init?: RequestInit): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json'
    }
    if (this.token) {
      headers.Authorization = `Bearer ${this.token}`
    }

    const res = await fetch(`${API_BASE}${path}`, {
      ...init,
      headers: {
        ...headers,
        ...(init?.headers ?? {})
      }
    })
    if (!res.ok) {
      if (this.token && res.status === 401) {
        window.dispatchEvent(new CustomEvent('gomi:auth-expired'))
        throw new Error('Session expired')
      }
      const payload = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
      throw new Error(payload.error ?? `HTTP ${res.status}`)
    }

    if (res.status === 204) {
      return undefined as T
    }

    return (await res.json()) as T
  }

  login(username: string, password: string) {
    return this.request<{ token: string }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password })
    })
  }

  setupStatus() {
    return this.request<{ required: boolean }>('/setup/status')
  }

  setupAdmin(username: string, password: string) {
    return this.request<{ token: string }>('/setup/admin', {
      method: 'POST',
      body: JSON.stringify({ username, password })
    })
  }

  logout() {
    return this.request<void>('/auth/logout', { method: 'POST' })
  }

  me() {
    return this.request<Me>('/me')
  }

  systemInfo() {
    return this.request<SystemInfo>('/system-info')
  }

  listMachines() {
    return this.request<ListResponse<'machines', MachineApi>>('/machines')
      .then((res) => ({ items: res.machines.map(fromApiMachine) }))
  }

  getMachine(name: string) {
    return this.request<MachineApi>(`/machines/${encodeURIComponent(name)}`).then(fromApiMachine)
  }

  createMachine(data: { name: string } & MachineSpecPayload & { hostname: string; mac: string; arch: string; firmware: string; power: PowerConfig; osPreset: Machine['osPreset'] }) {
    const { name, ...machine } = data
    const params = new URLSearchParams({ machineId: name })
    return this.request<MachineApi>(`/machines?${params.toString()}`, {
      method: 'POST',
      body: JSON.stringify({ machine: toApiMachineSpec(machine) })
    }).then(fromApiMachine)
  }

  deleteMachine(name: string) {
    return this.request<void>(`/machines/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  async redeploy(name: string, payload?: MachineRedeployPayload) {
    const body = JSON.stringify(payload ? toApiMachineSpec(payload) : {})
    return this.request<MachineApi>(`/machines/${encodeURIComponent(name)}:redeploy`, {
      method: 'POST',
      body
    }).then(fromApiMachine)
  }

  reinstall(name: string, payload?: MachineRedeployPayload) {
    return this.redeploy(name, payload)
  }

  powerOn(name: string) {
    return this.request<{ status: string }>(`/machines/${encodeURIComponent(name)}:powerOn`, {
      method: 'POST'
    })
  }

  powerOff(name: string) {
    return this.request<{ status: string }>(`/machines/${encodeURIComponent(name)}:powerOff`, {
      method: 'POST'
    })
  }

  updateMachineSettings(name: string, payload: { power: PowerConfig }) {
    return this.request<MachineApi>(`/machines/${encodeURIComponent(name)}/settings`, {
      method: 'PATCH',
      body: JSON.stringify(payload)
    }).then(fromApiMachine)
  }

  updateMachineNetwork(name: string, payload: { ip: string; ipAssignment: string; subnetRef: string; domain: string }) {
    return this.request<MachineApi>(`/machines/${encodeURIComponent(name)}/network`, {
      method: 'PATCH',
      body: JSON.stringify({ ...payload, subnetRef: resourceName('subnets', payload.subnetRef) })
    }).then(fromApiMachine)
  }

  listSubnets() {
    return this.request<ListResponse<'subnets', NamedApi<Subnet, 'subnetId'>>>('/subnets')
      .then((res) => ({ items: res.subnets.map((item) => fromApiNamed('subnets', 'subnetId', item)) }))
  }

  getSubnet(name: string) {
    return this.request<NamedApi<Subnet, 'subnetId'>>(`/subnets/${encodeURIComponent(name)}`)
      .then((item) => fromApiNamed('subnets', 'subnetId', item))
  }

  createSubnet(subnet: { name: string; spec: Subnet['spec'] }) {
    return this.request<NamedApi<Subnet, 'subnetId'>>('/subnets', {
      method: 'POST',
      body: JSON.stringify(subnet)
    }).then((item) => fromApiNamed('subnets', 'subnetId', item))
  }

  updateSubnet(name: string, spec: Subnet['spec']) {
    return this.request<NamedApi<Subnet, 'subnetId'>>(`/subnets/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify({ spec })
    }).then((item) => fromApiNamed('subnets', 'subnetId', item))
  }

  deleteSubnet(name: string) {
    return this.request<void>(`/subnets/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  getHardwareInfo(machineName: string) {
    return this.request<HardwareInfo>(`/machines/${encodeURIComponent(machineName)}/hardware`)
  }

  listSSHKeys() {
    return this.request<ListResponse<'sshKeys', NamedApi<SSHKey, 'sshKeyId'>>>('/ssh-keys')
      .then((res) => ({ items: res.sshKeys.map((item) => fromApiNamed('sshKeys', 'sshKeyId', item)) }))
  }

  createSSHKey(key: { name: string; publicKey: string; privateKey?: string; comment?: string }) {
    return this.request<NamedApi<SSHKey, 'sshKeyId'>>('/ssh-keys', {
      method: 'POST',
      body: JSON.stringify(key)
    }).then((item) => fromApiNamed('sshKeys', 'sshKeyId', item))
  }

  deleteSSHKey(name: string) {
    return this.request<void>(`/ssh-keys/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  listAudit(machine?: string) {
    const params = new URLSearchParams({ limit: '50' })
    if (machine && machine.trim().length > 0) {
      params.set('machine', machine)
    }
    return this.request<ListResponse<'auditEvents', AuditEvent>>(`/audit-events?${params.toString()}`)
      .then((res) => ({ items: res.auditEvents }))
  }

  // Hypervisor APIs
  listHypervisors() {
    return this.request<ListResponse<'hypervisors', NamedApi<Hypervisor, 'hypervisorId'>>>('/hypervisors')
      .then((res) => ({ items: res.hypervisors.map(fromApiHypervisor) }))
  }

  getHypervisor(name: string) {
    return this.request<NamedApi<Hypervisor, 'hypervisorId'>>(`/hypervisors/${encodeURIComponent(name)}`)
      .then(fromApiHypervisor)
  }

  createHypervisor(data: { name: string; connection: Hypervisor['connection']; labels?: Record<string, string> }) {
    return this.request<NamedApi<Hypervisor, 'hypervisorId'>>('/hypervisors', {
      method: 'POST',
      body: JSON.stringify(data)
    }).then(fromApiHypervisor)
  }

  deleteHypervisor(name: string) {
    return this.request<void>(`/hypervisors/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  createRegistrationToken() {
    return this.request<{ token: string; expiresAt: string }>('/hypervisors/registration-tokens', {
      method: 'POST'
    })
  }

  // Virtual Machine APIs
  listVirtualMachines() {
    return this.request<ListResponse<'virtualMachines', VirtualMachineApi>>('/virtual-machines')
      .then((res) => ({ items: res.virtualMachines.map(fromApiVirtualMachine) }))
  }

  getVirtualMachine(name: string) {
    return this.request<VirtualMachineApi>(`/virtual-machines/${encodeURIComponent(name)}`)
      .then(fromApiVirtualMachine)
  }

  createVirtualMachine(data: { name: string; hypervisorRef: string; resources: VirtualMachine['resources']; osImageRef?: string; cloudInitRefs?: string[]; cloudInitRef?: string; installCfg?: VirtualMachine['installCfg']; network?: VirtualMachine['network']; ipAssignment?: 'dhcp' | 'static'; subnetRef?: string; domain?: string; powerControlMethod: string; advancedOptions?: VirtualMachine['advancedOptions']; sshKeyRefs?: string[]; loginUser?: VirtualMachine['loginUser'] }) {
    const { name, installCfg, ...rest } = data
    const params = new URLSearchParams({ virtualMachineId: name })
    return this.request<VirtualMachineApi>(`/virtual-machines?${params.toString()}`, {
      method: 'POST',
      body: JSON.stringify({
        virtualMachine: {
          ...toApiVirtualMachineSpec(rest),
          installConfig: installCfg
        }
      })
    }).then(fromApiVirtualMachine)
  }

  deleteVirtualMachine(name: string) {
    return this.request<void>(`/virtual-machines/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  vmPowerOn(name: string) {
    return this.request<VirtualMachineApi>(`/virtual-machines/${encodeURIComponent(name)}:powerOn`, {
      method: 'POST'
    }).then(fromApiVirtualMachine)
  }

  vmPowerOff(name: string) {
    return this.request<VirtualMachineApi>(`/virtual-machines/${encodeURIComponent(name)}:powerOff`, {
      method: 'POST'
    }).then(fromApiVirtualMachine)
  }

  async vmRedeploy(name: string, payload?: VMRedeployPayload) {
    const body = JSON.stringify(payload ? toApiVirtualMachineSpec(payload) : {})
    return this.request<VirtualMachineApi>(`/virtual-machines/${encodeURIComponent(name)}:redeploy`, {
      method: 'POST',
      body
    }).then(fromApiVirtualMachine)
  }

  vmMigrate(name: string, payload?: { targetHypervisor?: string }) {
    return this.request<VirtualMachineApi>(`/virtual-machines/${encodeURIComponent(name)}:migrate`, {
      method: 'POST',
      body: JSON.stringify(payload ? { targetHypervisor: resourceName('hypervisors', payload.targetHypervisor) } : {})
    }).then(fromApiVirtualMachine)
  }

  vmReinstall(
    name: string,
    payload?: VMRedeployPayload
  ) {
    return this.vmRedeploy(name, payload)
  }

  // Cloud-Init Template APIs
  listCloudInitTemplates() {
    return this.request<ListResponse<'cloudInitTemplates', NamedApi<CloudInitTemplate, 'cloudInitTemplateId'>>>('/cloud-init-templates')
      .then((res) => ({ items: res.cloudInitTemplates.map((item) => fromApiNamed('cloudInitTemplates', 'cloudInitTemplateId', item)) }))
  }

  getCloudInitTemplate(name: string) {
    return this.request<NamedApi<CloudInitTemplate, 'cloudInitTemplateId'>>(`/cloud-init-templates/${encodeURIComponent(name)}`)
      .then((item) => fromApiNamed('cloudInitTemplates', 'cloudInitTemplateId', item))
  }

  createCloudInitTemplate(data: { name: string; description?: string; userData: string; networkConfig?: string; metadataTemplate?: string }) {
    return this.request<NamedApi<CloudInitTemplate, 'cloudInitTemplateId'>>('/cloud-init-templates', {
      method: 'POST',
      body: JSON.stringify(data)
    }).then((item) => fromApiNamed('cloudInitTemplates', 'cloudInitTemplateId', item))
  }

  updateCloudInitTemplate(name: string, data: { description?: string; userData: string; networkConfig?: string; metadataTemplate?: string }) {
    return this.request<NamedApi<CloudInitTemplate, 'cloudInitTemplateId'>>(`/cloud-init-templates/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify(data)
    }).then((item) => fromApiNamed('cloudInitTemplates', 'cloudInitTemplateId', item))
  }

  deleteCloudInitTemplate(name: string) {
    return this.request<void>(`/cloud-init-templates/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  // OS Image APIs
  listOSImages() {
    return this.request<ListResponse<'osImages', NamedApi<OSImage, 'osImageId'>>>('/os-images')
      .then((res) => ({ items: res.osImages.map(fromApiOSImage) }))
  }

  listBootEnvironments() {
    return this.request<ListResponse<'bootEnvironments', NamedApi<BootEnvironmentStatus, 'bootEnvironmentId'>>>('/boot-environments')
      .then((res) => ({ items: res.bootEnvironments.map((item) => fromApiNamed('bootEnvironments', 'bootEnvironmentId', item)) }))
  }

  getOSImage(name: string) {
    return this.request<NamedApi<OSImage, 'osImageId'>>(`/os-images/${encodeURIComponent(name)}`)
      .then(fromApiOSImage)
  }

  createOSImage(data: { name: string; osFamily: string; osVersion: string; arch: string; format: string; source: string; url?: string; checksum?: string }) {
    return this.request<NamedApi<OSImage, 'osImageId'>>('/os-images', {
      method: 'POST',
      body: JSON.stringify(data)
    }).then(fromApiOSImage)
  }

  uploadOSImageFile(name: string, file: File) {
    const form = new FormData()
    form.append('file', file)
    const headers: Record<string, string> = {}
    if (this.token) {
      headers.Authorization = `Bearer ${this.token}`
    }
    return fetch(`${API_BASE}/os-images/${encodeURIComponent(name)}/upload`, {
      method: 'POST',
      headers,
      body: form
    }).then(async (res) => {
      if (!res.ok) {
        const payload = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
        throw new Error(payload.error ?? `HTTP ${res.status}`)
      }
      return (res.json() as Promise<NamedApi<OSImage, 'osImageId'>>).then(fromApiOSImage)
    })
  }

  deleteOSImage(name: string) {
    return this.request<void>(`/os-images/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  // DHCP Lease APIs
  listDHCPLeases() {
    return this.request<ListResponse<'dhcpLeases', DHCPLease>>('/dhcp-leases')
      .then((res) => ({ items: res.dhcpLeases }))
  }

  listDNSRecords() {
    return this.request<ListResponse<'dnsRecords', DNSRecord>>('/dns-records')
      .then((res) => ({ items: res.dnsRecords }))
  }

  createDNSRecord(record: Pick<DNSRecord, 'name' | 'type' | 'ttl' | 'values'>) {
    return this.request<DNSRecord>('/dns-records', {
      method: 'POST',
      body: JSON.stringify(record)
    })
  }

  updateDNSRecord(record: Pick<DNSRecord, 'name' | 'type' | 'ttl' | 'values'>) {
    return this.request<DNSRecord>(`/dns-records/${encodeURIComponent(record.name)}/${encodeURIComponent(record.type)}`, {
      method: 'PUT',
      body: JSON.stringify(record)
    })
  }

  deleteDNSRecord(name: string, type: DNSRecord['type']) {
    return this.request<void>(`/dns-records/${encodeURIComponent(name)}/${encodeURIComponent(type)}`, {
      method: 'DELETE'
    })
  }
}

export const api = new ApiClient()
