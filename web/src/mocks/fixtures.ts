import type { AuditEvent, DNSRecord, Machine, Subnet } from '../types'

// gpu-worker-01 simulates a live attempt, so its timings are generated
// relative to page load.
const nowMs = Date.now()
function sinceStart(seconds: number): string {
  return new Date(nowMs - 240_000 + seconds * 1000).toISOString()
}

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
    osPreset: { family: 'ubuntu', version: '24.04', imageRef: 'ubuntu-24.04-amd64-baremetal' },
    ipAssignment: 'static',
    subnetRef: 'default',
    phase: 'Ready',
    powerState: 'running',
    provision: {
      message: 'Install completed successfully',
      startedAt: '2025-12-10T08:00:00Z',
      completedAt: '2025-12-10T09:12:00Z',
      lastSignalAt: '2025-12-10T09:12:00Z',
      trigger: 'manual',
      requestedBy: 'admin',
      attemptId: 'attempt-node01-3f2a',
      completionSource: 'curtin',
      timings: [
        { source: 'server', name: 'server.pxe.boot_script', eventType: 'marker', message: 'PXE boot script served (installer)', timestamp: '2025-12-10T08:00:55Z' },
        { source: 'initramfs', name: 'initramfs.init_top', eventType: 'marker', message: 'initramfs init-top reached', monotonicSeconds: 4.28 },
        { source: 'runner', name: 'runner.dhcp', eventType: 'timing', message: 'waiting for DHCP lease', result: 'success', startedAt: '2025-12-10T08:01:20Z', finishedAt: '2025-12-10T08:01:23Z', durationMs: 3000 },
        { source: 'runner', name: 'runner.inventory', eventType: 'timing', message: 'posting hardware inventory', result: 'success', startedAt: '2025-12-10T08:01:24Z', finishedAt: '2025-12-10T08:01:26Z', durationMs: 2100 },
        { source: 'server', name: 'server.inventory.total', eventType: 'timing', message: 'store hardware inventory', result: 'success', startedAt: '2025-12-10T08:01:26Z', finishedAt: '2025-12-10T08:01:27Z', durationMs: 620 },
        { source: 'server', name: 'server.curtin_config', eventType: 'timing', message: 'generate curtin install config', result: 'success', startedAt: '2025-12-10T08:01:40Z', finishedAt: '2025-12-10T08:01:41Z', durationMs: 400 },
        { source: 'server', name: 'server.artifact_transfer', eventType: 'timing', message: 'serve OS artifact rootfs.squashfs (812345678 bytes)', result: 'success', startedAt: '2025-12-10T08:01:45Z', finishedAt: '2025-12-10T08:04:30Z', durationMs: 165000 },
        { source: 'curtin', name: 'cmd-install/stage-extract', eventType: 'finish', message: 'writing install sources to disk', result: 'success', startedAt: '2025-12-10T08:04:40Z', finishedAt: '2025-12-10T08:58:00Z', durationMs: 3200000 },
        { source: 'curtin', name: 'cmd-install/stage-late', eventType: 'finish', message: 'configuring bootloader', result: 'success', startedAt: '2025-12-10T08:58:00Z', finishedAt: '2025-12-10T09:01:50Z', durationMs: 230000 },
        { source: 'unknown', name: 'image_applied', eventType: 'image_applied', message: 'image applied; waiting for target OS first boot', timestamp: '2025-12-10T09:02:00Z' },
        { source: 'server', name: 'server.pxe.boot_script_local', eventType: 'marker', message: 'PXE boot script served (local boot after image apply)', timestamp: '2025-12-10T09:03:10Z' },
        { source: 'server', name: 'server.reboot_to_os', eventType: 'timing', message: 'reboot into target OS, first boot, and readiness wait', result: 'success', startedAt: '2025-12-10T09:02:00Z', finishedAt: '2025-12-10T09:12:00Z', durationMs: 600000 },
        { source: 'server', name: 'server.install_complete', eventType: 'marker', message: 'install-complete received (curtin)', timestamp: '2025-12-10T09:12:00Z' }
      ]
    },
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
    osPreset: { family: 'ubuntu', version: '24.04', imageRef: 'ubuntu-24.04-amd64-baremetal' },
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
    osPreset: { family: 'ubuntu', version: '22.04', imageRef: 'ubuntu-22.04-amd64-baremetal' },
    ipAssignment: 'static',
    subnetRef: 'default',
    phase: 'Provisioning',
    provision: {
      active: true,
      message: 'Downloading image...',
      startedAt: sinceStart(0),
      lastSignalAt: sinceStart(220),
      trigger: 'manual',
      requestedBy: 'admin',
      attemptId: 'attempt-gpu01-91cd',
      timings: [
        { source: 'server', name: 'server.pxe.boot_script', eventType: 'marker', message: 'PXE boot script served (installer)', timestamp: sinceStart(45) },
        { source: 'initramfs', name: 'initramfs.init_top', eventType: 'marker', message: 'initramfs init-top reached', monotonicSeconds: 4.28 },
        { source: 'runner', name: 'runner.dhcp', eventType: 'timing', message: 'waiting for DHCP lease', result: 'success', startedAt: sinceStart(80), finishedAt: sinceStart(83), durationMs: 3000 },
        { source: 'runner', name: 'runner.inventory', eventType: 'timing', message: 'posting hardware inventory', result: 'success', startedAt: sinceStart(84), finishedAt: sinceStart(86), durationMs: 2100 },
        { source: 'server', name: 'server.inventory.store', eventType: 'timing', message: 'store hardware inventory', result: 'success', startedAt: sinceStart(86), finishedAt: sinceStart(87), durationMs: 620 },
        { source: 'server', name: 'server.curtin_config', eventType: 'timing', message: 'generate curtin install config', result: 'success', startedAt: sinceStart(95), finishedAt: sinceStart(96), durationMs: 400 },
        { source: 'server', name: 'server.artifact_transfer', eventType: 'timing', message: 'serve OS artifact rootfs.squashfs', result: 'success', startedAt: sinceStart(100), finishedAt: sinceStart(220), durationMs: 120000 }
      ]
    },
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
    osPreset: { family: 'debian', version: '13', imageRef: 'debian-13-amd64-baremetal' },
    ipAssignment: 'dhcp',
    phase: 'Error',
    powerState: 'stopped',
    lastError: 'curtin install failed: exit status 1',
    // Legacy attempt recorded before the server boundary markers existed: the
    // head interval surfaces as an explicit untracked segment.
    provision: {
      message: 'deploy failed',
      startedAt: '2025-12-09T22:00:00Z',
      finishedAt: '2025-12-09T22:18:30Z',
      lastSignalAt: '2025-12-09T22:18:30Z',
      failureReason: 'curtin install failed: exit status 1',
      trigger: 'redeploy',
      requestedBy: 'admin',
      attemptId: 'attempt-edge01-77aa',
      timings: [
        { source: 'runner', name: 'runner.dhcp', eventType: 'timing', message: 'waiting for DHCP lease', result: 'success', startedAt: '2025-12-09T22:02:05Z', finishedAt: '2025-12-09T22:02:08Z', durationMs: 3000 },
        { source: 'runner', name: 'runner.inventory', eventType: 'timing', message: 'posting hardware inventory', result: 'success', startedAt: '2025-12-09T22:02:09Z', finishedAt: '2025-12-09T22:02:11Z', durationMs: 2100 },
        { source: 'server', name: 'server.inventory.total', eventType: 'timing', message: 'store hardware inventory', result: 'success', startedAt: '2025-12-09T22:02:11Z', finishedAt: '2025-12-09T22:02:12Z', durationMs: 610 },
        { source: 'server', name: 'server.artifact_transfer', eventType: 'timing', message: 'serve OS artifact rootfs.squashfs', result: 'success', startedAt: '2025-12-09T22:02:20Z', finishedAt: '2025-12-09T22:05:00Z', durationMs: 160000 },
        { source: 'curtin', name: 'cmd-install/stage-partitioning', eventType: 'failed', message: 'exit status 1', result: 'failure', timestamp: '2025-12-09T22:18:30Z' }
      ]
    },
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
    osPreset: { family: 'ubuntu', version: '24.04', imageRef: 'ubuntu-24.04-amd64-baremetal' },
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

export const dnsRecords: DNSRecord[] = [
  {
    name: 'app.lab.internal.',
    type: 'A',
    ttl: 300,
    values: ['10.0.0.60'],
    createdAt: '2025-12-10T06:00:00Z',
    updatedAt: '2025-12-10T06:00:00Z'
  },
  {
    name: 'grafana.lab.internal.',
    type: 'CNAME',
    ttl: 300,
    values: ['app.lab.internal.'],
    createdAt: '2025-12-10T06:05:00Z',
    updatedAt: '2025-12-10T06:05:00Z'
  },
  {
    name: 'owner.lab.internal.',
    type: 'TXT',
    ttl: 300,
    values: ['managed-by=gomi'],
    createdAt: '2025-12-10T06:10:00Z',
    updatedAt: '2025-12-10T06:10:00Z'
  }
]
