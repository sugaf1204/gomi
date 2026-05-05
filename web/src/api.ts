import type { AuditEvent, BootEnvironmentStatus, CloudInitTemplate, DHCPLease, HardwareInfo, Hypervisor, Machine, Me, OSCatalogItem, OSImage, PowerConfig, SSHKey, Subnet, SystemInfo, VirtualMachine } from './types'

const API_BASE = import.meta.env.VITE_API_BASE ?? `${window.location.origin}/api/v1`

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
    return this.request<{ items: Machine[] }>('/machines')
  }

  getMachine(name: string) {
    return this.request<Machine>(`/machines/${encodeURIComponent(name)}`)
  }

  createMachine(data: { name: string } & MachineSpecPayload & { hostname: string; mac: string; arch: string; firmware: string; power: PowerConfig; osPreset: Machine['osPreset'] }) {
    return this.request<Machine>('/machines', {
      method: 'POST',
      body: JSON.stringify(data)
    })
  }

  deleteMachine(name: string) {
    return this.request<void>(`/machines/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  async redeploy(name: string, payload?: MachineRedeployPayload) {
    const body = JSON.stringify(payload ?? {})
    try {
      return await this.request<Machine>(`/machines/${encodeURIComponent(name)}/actions/redeploy`, {
        method: 'POST',
        body
      })
    } catch (err) {
      if (!(err instanceof Error) || !/HTTP 404|Not Found/i.test(err.message)) {
        throw err
      }
      return this.request<Machine>(`/machines/${encodeURIComponent(name)}/actions/reinstall`, {
        method: 'POST',
        body
      })
    }
  }

  reinstall(name: string, payload?: MachineRedeployPayload) {
    return this.redeploy(name, payload)
  }

  powerOn(name: string) {
    return this.request<{ status: string }>(`/machines/${encodeURIComponent(name)}/actions/power-on`, {
      method: 'POST'
    })
  }

  powerOff(name: string) {
    return this.request<{ status: string }>(`/machines/${encodeURIComponent(name)}/actions/power-off`, {
      method: 'POST'
    })
  }

  updateMachineSettings(name: string, payload: { power: PowerConfig }) {
    return this.request<Machine>(`/machines/${encodeURIComponent(name)}/settings`, {
      method: 'PATCH',
      body: JSON.stringify(payload)
    })
  }

  updateMachineNetwork(name: string, payload: { ip: string; ipAssignment: string; subnetRef: string; domain: string }) {
    return this.request<Machine>(`/machines/${encodeURIComponent(name)}/network`, {
      method: 'PATCH',
      body: JSON.stringify(payload)
    })
  }

  listSubnets() {
    return this.request<{ items: Subnet[] }>('/subnets')
  }

  getSubnet(name: string) {
    return this.request<Subnet>(`/subnets/${encodeURIComponent(name)}`)
  }

  createSubnet(subnet: { name: string; spec: Subnet['spec'] }) {
    return this.request<Subnet>('/subnets', {
      method: 'POST',
      body: JSON.stringify(subnet)
    })
  }

  updateSubnet(name: string, spec: Subnet['spec']) {
    return this.request<Subnet>(`/subnets/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify({ spec })
    })
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
    return this.request<{ items: SSHKey[] }>('/ssh-keys')
  }

  createSSHKey(key: { name: string; publicKey: string; privateKey?: string; comment?: string }) {
    return this.request<SSHKey>('/ssh-keys', {
      method: 'POST',
      body: JSON.stringify(key)
    })
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
    return this.request<{ items: AuditEvent[] }>(`/audit-events?${params.toString()}`)
  }

  // Hypervisor APIs
  listHypervisors() {
    return this.request<{ items: Hypervisor[] }>('/hypervisors')
  }

  getHypervisor(name: string) {
    return this.request<Hypervisor>(`/hypervisors/${encodeURIComponent(name)}`)
  }

  createHypervisor(data: { name: string; connection: Hypervisor['connection']; labels?: Record<string, string> }) {
    return this.request<Hypervisor>('/hypervisors', {
      method: 'POST',
      body: JSON.stringify(data)
    })
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
    return this.request<{ items: VirtualMachine[] }>('/virtual-machines')
  }

  getVirtualMachine(name: string) {
    return this.request<VirtualMachine>(`/virtual-machines/${encodeURIComponent(name)}`)
  }

  createVirtualMachine(data: { name: string; hypervisorRef: string; resources: VirtualMachine['resources']; osImageRef?: string; cloudInitRefs?: string[]; cloudInitRef?: string; installCfg?: VirtualMachine['installCfg']; network?: VirtualMachine['network']; ipAssignment?: 'dhcp' | 'static'; subnetRef?: string; domain?: string; powerControlMethod: string; advancedOptions?: VirtualMachine['advancedOptions']; sshKeyRefs?: string[]; loginUser?: VirtualMachine['loginUser'] }) {
    return this.request<VirtualMachine>('/virtual-machines', {
      method: 'POST',
      body: JSON.stringify(data)
    })
  }

  deleteVirtualMachine(name: string) {
    return this.request<void>(`/virtual-machines/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  vmPowerOn(name: string) {
    return this.request<{ status: string }>(`/virtual-machines/${encodeURIComponent(name)}/actions/power-on`, {
      method: 'POST'
    })
  }

  vmPowerOff(name: string) {
    return this.request<{ status: string }>(`/virtual-machines/${encodeURIComponent(name)}/actions/power-off`, {
      method: 'POST'
    })
  }

  async vmRedeploy(name: string, payload?: VMRedeployPayload) {
    const body = JSON.stringify(payload ?? {})
    try {
      return await this.request<VirtualMachine>(`/virtual-machines/${encodeURIComponent(name)}/actions/redeploy`, {
        method: 'POST',
        body
      })
    } catch (err) {
      // Fallback for backends exposing only the reinstall route.
      if (!(err instanceof Error) || !/HTTP 404|Not Found/i.test(err.message)) {
        throw err
      }
      return this.request<VirtualMachine>(`/virtual-machines/${encodeURIComponent(name)}/actions/reinstall`, {
        method: 'POST',
        body
      })
    }
  }

  vmMigrate(name: string, payload?: { targetHypervisor?: string }) {
    return this.request<VirtualMachine>(`/virtual-machines/${encodeURIComponent(name)}/actions/migrate`, {
      method: 'POST',
      body: JSON.stringify(payload ?? {})
    })
  }

  vmReinstall(
    name: string,
    payload?: VMRedeployPayload
  ) {
    return this.vmRedeploy(name, payload)
  }

  // Cloud-Init Template APIs
  listCloudInitTemplates() {
    return this.request<{ items: CloudInitTemplate[] }>('/cloud-init-templates')
  }

  getCloudInitTemplate(name: string) {
    return this.request<CloudInitTemplate>(`/cloud-init-templates/${encodeURIComponent(name)}`)
  }

  createCloudInitTemplate(data: { name: string; description?: string; userData: string; networkConfig?: string; metadataTemplate?: string }) {
    return this.request<CloudInitTemplate>('/cloud-init-templates', {
      method: 'POST',
      body: JSON.stringify(data)
    })
  }

  updateCloudInitTemplate(name: string, data: { description?: string; userData: string; networkConfig?: string; metadataTemplate?: string }) {
    return this.request<CloudInitTemplate>(`/cloud-init-templates/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify(data)
    })
  }

  deleteCloudInitTemplate(name: string) {
    return this.request<void>(`/cloud-init-templates/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  // OS Image APIs
  listOSImages() {
    return this.request<{ items: OSImage[] }>('/os-images')
  }

  listOSCatalog() {
    return this.request<{ items: OSCatalogItem[] }>('/os-catalog')
  }

  installOSCatalogEntry(name: string) {
    return this.request<OSCatalogItem>(`/os-catalog/${encodeURIComponent(name)}/install`, {
      method: 'POST'
    })
  }

  listBootEnvironments() {
    return this.request<{ items: BootEnvironmentStatus[] }>('/boot-environments')
  }

  getOSImage(name: string) {
    return this.request<OSImage>(`/os-images/${encodeURIComponent(name)}`)
  }

  createOSImage(data: { name: string; osFamily: string; osVersion: string; arch: string; format: string; source: string; url?: string; checksum?: string }) {
    return this.request<OSImage>('/os-images', {
      method: 'POST',
      body: JSON.stringify(data)
    })
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
      return res.json() as Promise<OSImage>
    })
  }

  deleteOSImage(name: string) {
    return this.request<void>(`/os-images/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    })
  }

  // DHCP Lease APIs
  listDHCPLeases() {
    return this.request<{ items: DHCPLease[] }>('/dhcp-leases')
  }
}

export const api = new ApiClient()
