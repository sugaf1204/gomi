export type PowerType = 'ipmi' | 'webhook' | 'wol' | 'manual'
export type PowerState = 'unknown' | 'running' | 'stopped'

export type IPMIConfig = {
  host: string
  username: string
  password?: string
  interface?: string
}

export type WebhookConfig = {
  powerOnURL: string
  powerOffURL: string
  statusURL?: string
  bootOrderURL?: string
  headers?: Record<string, string>
  bodyExtras?: Record<string, string>
}

export type WoLConfig = {
  wakeMAC: string
  broadcastIP?: string
  port?: number
  shutdownTarget?: string
  shutdownUDPPort?: number
  hmacSecret?: string
  token?: string
  tokenTTLSeconds?: number
}

export type ManualConfig = Record<string, never>

export type PowerConfig = {
  type: PowerType
  ipmi?: IPMIConfig
  webhook?: WebhookConfig
  wol?: WoLConfig
  manual?: ManualConfig
}

export type IPMIConfigResponse = Omit<IPMIConfig, 'password'> & {
  passwordConfigured?: boolean
}

export type WebhookConfigResponse = Omit<WebhookConfig, 'headers' | 'bodyExtras'> & {
  headersConfigured?: boolean
  bodyExtrasConfigured?: boolean
}

export type WoLConfigResponse = Omit<WoLConfig, 'hmacSecret' | 'token'> & {
  hmacSecretConfigured?: boolean
  tokenConfigured?: boolean
}

export type PowerConfigResponse = {
  type: PowerType
  ipmi?: IPMIConfigResponse
  webhook?: WebhookConfigResponse
  wol?: WoLConfigResponse
  manual?: ManualConfig
}

export type Machine = {
  name: string
  hostname: string
  mac: string
  ip?: string
  arch: string
  firmware: 'uefi' | 'bios'
  power: PowerConfigResponse
  network?: { domain: string }
  osPreset: {
    family: string
    version: string
    imageRef: string
  }
  targetDisk?: string
  cloudInitRefs?: string[]
  ipAssignment?: 'dhcp' | 'static'
  subnetRef?: string
  role?: 'hypervisor' | ''
  bridgeName?: string
  sshKeyRefs?: string[]
  loginUser?: LoginUserSpec
  phase: string
  provision?: {
    startedAt?: string
    deadlineAt?: string
    finishedAt?: string
    completedAt?: string
    trigger?: string
    requestedBy?: string
    message?: string
    artifacts?: Record<string, string>
    timings?: ProvisionTiming[]
  }
  powerState?: PowerState
  powerStateAt?: string
  lastPowerAction?: string
  lastDeployedCloudInitRef?: string
  lastError?: string
  updatedAt?: string
}

export type ProvisionTiming = {
  source?: string
  name: string
  eventType?: string
  message?: string
  result?: string
  timestamp?: string
  startedAt?: string
  finishedAt?: string
  durationMs?: number
  monotonicSeconds?: number
}

export type AuditEvent = {
  id: string
  machine: string
  action: string
  actor: string
  result: string
  message?: string
  createdAt: string
}

export type AddressRange = { start: string; end: string }

export type Subnet = {
  name: string
  spec: {
    cidr: string
    pxeInterface?: string
    pxeAddressRange?: AddressRange
    defaultGateway?: string
    dnsServers?: string[]
    dnsSearchDomains?: string[]
    vlanId?: number
    reservedRanges?: AddressRange[]
    leaseTime?: number
    domainName?: string
    ntpServers?: string[]
  }
  createdAt?: string
  updatedAt?: string
}

export type Me = {
  username: string
  role: string
}

export type HardwareInfo = {
  name: string
  machineName: string
  cpu: { model: string; cores: number; threads: number; arch: string; mhz?: string }
  memory: { totalMB: number; slots?: number }
  disks?: { name: string; sizeMB: number; type?: string; model?: string; serial?: string }[]
  nics?: { name: string; mac: string; speed?: string; state?: string }[]
  bios?: { vendor?: string; version?: string; date?: string }
  pci?: { slot: string; class: string; vendor: string; device: string }[]
  usb?: { bus: string; device: string; vendor?: string; name: string }[]
  gpus?: { name: string; vendor?: string; vram?: string }[]
  sensors?: { name: string; type: string; value: number; unit: string }[]
  createdAt?: string
  updatedAt?: string
}

export type SSHKey = {
  name: string
  publicKey: string
  privateKey?: string
  comment?: string
  keyType?: string
  createdAt?: string
  updatedAt?: string
}

export type LoginUserSpec = {
  username: string
  password?: string
}

export type Hypervisor = {
  name: string
  connection: {
    type: 'tcp'
    host: string
    port?: number
  }
  labels?: Record<string, string>
  machineRef?: string
  bridgeName?: string
  phase: 'Pending' | 'Registered' | 'Ready' | 'NotReady' | 'Unreachable'
  capacity?: {
    cpuCores: number
    memoryMB: number
    storageGB?: number
  }
  used?: {
    cpuUsedCores: number
    memoryUsedMB: number
    storageUsedGB?: number
  }
  vmCount: number
  libvirtURI?: string
  lastHeartbeat?: string
  lastError?: string
  createdAt?: string
  updatedAt?: string
}

export type VirtualMachine = {
  name: string
  hypervisorRef: string
  resources: { cpuCores: number; memoryMB: number; diskGB: number }
  osImageRef?: string
  cloudInitRefs?: string[]
  cloudInitRef?: string
  installCfg?: {
    type: 'preseed' | 'curtin'
    inline?: string
  }
  network?: { name: string; mac?: string; bridge?: string; network?: string; ipAddress?: string }[]
  ipAssignment?: 'dhcp' | 'static'
  subnetRef?: string
  domain?: string
  powerControlMethod: 'libvirt'
  advancedOptions?: {
    cpuPinning?: Record<number, string>
    cpuMode?: 'host-passthrough' | 'host-model' | 'maximum'
    ioThreads?: number
    diskDriver?: 'virtio' | 'scsi'
    diskFormat?: 'qcow2'
    netMultiqueue?: number
  }
  sshKeyRefs?: string[]
  loginUser?: LoginUserSpec
  phase: 'Pending' | 'Creating' | 'Running' | 'Stopped' | 'Provisioning' | 'Error' | 'Deleting' | 'Migrating'
  libvirtDomain?: string
  hypervisorName?: string
  ipAddresses?: string[]
  networkInterfaces?: { name?: string; mac?: string; ipAddresses?: string[] }[]
  provisioning?: {
    active?: boolean
    startedAt?: string
    deadlineAt?: string
    completedAt?: string
    completionToken?: string
    completionSource?: string
    lastSignalAt?: string
  }
  lastPowerAction?: string
  lastDeployedCloudInitRef?: string
  lastError?: string
  createdOnHost?: string
  createdAt?: string
  updatedAt?: string
}

export type CloudInitTemplate = {
  name: string
  description?: string
  userData: string
  networkConfig?: string
  metadataTemplate?: string
  createdAt?: string
  updatedAt?: string
}

export type OSImage = {
  name: string
  osFamily: string
  osVersion: string
  arch: string
  format: 'qcow2' | 'raw' | 'iso'
  variant?: 'cloud' | 'baremetal' | 'server' | 'desktop'
  source: 'upload' | 'url'
  url?: string
  checksum?: string
  sizeBytes?: number
  description?: string
  ready: boolean
  localPath?: string
  error?: string
  createdAt?: string
  updatedAt?: string
}

export type BootEnvironmentStatus = {
  name: string
  phase: 'missing' | 'building' | 'ready' | 'error'
  message?: string
  artifactDir?: string
  logPath?: string
  kernelPath?: string
  initrdPath?: string
  rootfsPath?: string
  updatedAt?: string
}

export type OSCatalogEntry = {
  name: string
  osFamily: string
  osVersion: string
  arch: string
  format: 'qcow2' | 'raw' | 'iso'
  sourceFormat?: 'qcow2' | 'raw' | 'iso'
  sourceCompression?: 'zstd'
  variant?: 'cloud' | 'baremetal' | 'server' | 'desktop'
  url: string
  checksum?: string
  description?: string
  bootEnvironment: string
}

export type OSCatalogItem = {
  entry: OSCatalogEntry
  installed: boolean
  installing: boolean
  osImageReady: boolean
  osImageError?: string
  bootEnvironment: BootEnvironmentStatus
}

export type DHCPLease = {
  mac: string
  ip: string
  hostname?: string
  pxeClient: boolean
  leasedAt: string
}

export type SystemInfo = {
  hostname: string
  os: string
  arch: string
  goVersion: string
  cpuCount: number
  goroutines: number
  uptime: number
  memoryUsedMB: number
}
