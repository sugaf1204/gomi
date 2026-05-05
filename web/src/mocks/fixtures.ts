import type { AuditEvent, Machine, Subnet } from '../types'

export const machines: Machine[] = [
  {
    name: 'node-01',
    hostname: 'node-01.lab.internal',
    mac: 'aa:bb:cc:dd:ee:01',
    ip: '10.0.0.11',
    arch: 'amd64',
    firmware: 'uefi',
    power: { type: 'webhook', webhook: { powerOnURL: 'http://power-mgmt.lab.internal:9090/power/on', powerOffURL: 'http://power-mgmt.lab.internal:9090/power/off' } },
    network: { domain: 'lab.internal' },
    osPreset: { family: 'ubuntu', version: '24.04', imageRef: 'ubuntu-2404-base' },
    ipAssignment: 'static',
    subnetRef: 'default',
    phase: 'Ready',
    powerState: 'running',
    provision: { message: 'Install completed successfully', startedAt: '2025-12-10T08:00:00Z', finishedAt: '2025-12-10T09:12:00Z', trigger: 'manual', requestedBy: 'admin' },
    lastPowerAction: 'power-on',
    updatedAt: '2025-12-10T09:15:00Z'
  },
  {
    name: 'node-02',
    hostname: 'node-02.lab.internal',
    mac: 'aa:bb:cc:dd:ee:02',
    ip: '10.0.0.12',
    arch: 'amd64',
    firmware: 'uefi',
    power: { type: 'webhook', webhook: { powerOnURL: 'http://power-mgmt.lab.internal:9090/power/on', powerOffURL: 'http://power-mgmt.lab.internal:9090/power/off' } },
    network: { domain: 'lab.internal' },
    osPreset: { family: 'ubuntu', version: '24.04', imageRef: 'ubuntu-2404-base' },
    ipAssignment: 'dhcp',
    phase: 'Ready',
    powerState: 'running',
    updatedAt: '2025-12-10T08:45:00Z'
  },
  {
    name: 'gpu-worker-01',
    hostname: 'gpu-worker-01.lab.internal',
    mac: 'aa:bb:cc:dd:ee:03',
    ip: '10.0.0.20',
    arch: 'amd64',
    firmware: 'uefi',
    power: { type: 'ipmi', ipmi: { host: '10.0.0.200', username: 'ipmi-admin' } },
    network: { domain: 'lab.internal' },
    osPreset: { family: 'ubuntu', version: '22.04', imageRef: 'ubuntu-2204-gpu' },
    ipAssignment: 'static',
    subnetRef: 'default',
    phase: 'Provisioning',
    provision: { message: 'Downloading image...', startedAt: '2025-12-10T10:00:00Z', trigger: 'manual', requestedBy: 'admin' },
    updatedAt: '2025-12-10T10:01:00Z'
  },
  {
    name: 'edge-01',
    hostname: 'edge-01.lab.internal',
    mac: 'aa:bb:cc:dd:ee:04',
    arch: 'arm64',
    firmware: 'uefi',
    power: { type: 'manual' },
    network: { domain: 'lab.internal' },
    osPreset: { family: 'debian', version: '13', imageRef: 'debian-13-amd64' },
    ipAssignment: 'dhcp',
    phase: 'Error',
    powerState: 'stopped',
    lastError: 'IPMI unreachable: connection timed out after 10s',
    updatedAt: '2025-12-09T22:30:00Z'
  },
  {
    name: 'storage-01',
    hostname: 'storage-01.lab.internal',
    mac: 'aa:bb:cc:dd:ee:05',
    ip: '10.0.0.50',
    arch: 'amd64',
    firmware: 'bios',
    power: { type: 'webhook', webhook: { powerOnURL: 'http://power-mgmt.lab.internal:9090/power/on', powerOffURL: 'http://power-mgmt.lab.internal:9090/power/off' } },
    network: { domain: 'lab.internal' },
    osPreset: { family: 'ubuntu', version: '24.04', imageRef: 'ubuntu-2404-base' },
    ipAssignment: 'static',
    subnetRef: 'lab-vlan100',
    phase: 'Ready',
    powerState: 'running',
    updatedAt: '2025-12-10T07:00:00Z'
  }
]

export const auditEvents: AuditEvent[] = [
  { id: 'evt-001', machine: 'node-01', action: 'redeploy', actor: 'admin', result: 'success', message: 'Triggered via console', createdAt: '2025-12-10T07:59:00Z' },
  { id: 'evt-002', machine: 'node-01', action: 'power-on', actor: 'admin', result: 'success', createdAt: '2025-12-10T09:14:00Z' },
  { id: 'evt-003', machine: 'node-02', action: 'redeploy', actor: 'operator-bot', result: 'success', createdAt: '2025-12-10T06:28:00Z' },
  { id: 'evt-004', machine: 'gpu-worker-01', action: 'redeploy', actor: 'admin', result: 'success', createdAt: '2025-12-10T09:59:00Z' },
  { id: 'evt-005', machine: 'edge-01', action: 'redeploy', actor: 'admin', result: 'failure', message: 'PXE boot timeout', createdAt: '2025-12-09T20:58:00Z' },
  { id: 'evt-006', machine: 'edge-01', action: 'power-on', actor: 'admin', result: 'failure', message: 'IPMI unreachable', createdAt: '2025-12-09T22:28:00Z' },
  { id: 'evt-007', machine: 'storage-01', action: 'power-off', actor: 'admin', result: 'success', createdAt: '2025-12-10T06:55:00Z' },
  { id: 'evt-008', machine: 'storage-01', action: 'power-on', actor: 'admin', result: 'success', createdAt: '2025-12-10T06:58:00Z' },
  { id: 'evt-009', machine: 'node-01', action: 'settings-update', actor: 'admin', result: 'success', message: 'Changed power type from ipmi to webhook', createdAt: '2025-12-10T05:30:00Z' },
  { id: 'evt-010', machine: 'node-02', action: 'power-on', actor: 'operator-bot', result: 'success', createdAt: '2025-12-10T08:44:00Z' },
  { id: 'evt-011', machine: 'gpu-worker-01', action: 'power-on', actor: 'admin', result: 'success', createdAt: '2025-12-10T09:58:00Z' },
  { id: 'evt-012', machine: 'node-01', action: 'power-off', actor: 'admin', result: 'success', createdAt: '2025-12-09T23:00:00Z' }
]

export const subnets: Subnet[] = [
  {
    name: 'default',
    spec: {
      cidr: '10.0.0.0/24',
      pxeInterface: 'eth0',
      pxeAddressRange: { start: '10.0.0.200', end: '10.0.0.250' },
      defaultGateway: '10.0.0.1',
      dnsServers: ['10.0.0.1', '8.8.8.8'],
      dnsSearchDomains: ['lab.internal']
    },
    createdAt: '2025-12-01T00:00:00Z',
    updatedAt: '2025-12-01T00:00:00Z'
  },
  {
    name: 'lab-vlan100',
    spec: {
      cidr: '10.100.0.0/24',
      defaultGateway: '10.100.0.1',
      dnsServers: ['10.100.0.1'],
      vlanId: 100,
      reservedRanges: [{ start: '10.100.0.1', end: '10.100.0.10' }]
    },
    createdAt: '2025-12-05T12:00:00Z',
    updatedAt: '2025-12-05T12:00:00Z'
  }
]
