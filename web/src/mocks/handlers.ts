import { http, HttpResponse } from 'msw'
import { auditEvents, machines, subnets } from './fixtures'

const API_BASE = 'http://localhost:5392/api/v1'

export const handlers = [
  http.post(`${API_BASE}/auth/login`, () => {
    return HttpResponse.json({ token: 'mock-token-abc123' })
  }),

  http.get(`${API_BASE}/setup/status`, () => {
    return HttpResponse.json({ required: false })
  }),

  http.post(`${API_BASE}/setup/admin`, () => {
    return HttpResponse.json({ token: 'mock-token-abc123' }, { status: 201 })
  }),

  http.post(`${API_BASE}/auth/logout`, () => {
    return new HttpResponse(null, { status: 204 })
  }),

  http.get(`${API_BASE}/me`, () => {
    return HttpResponse.json({ username: 'admin', role: 'admin' })
  }),

  http.get(`${API_BASE}/machines`, () => {
    return HttpResponse.json({ items: machines })
  }),

  http.get(`${API_BASE}/machines/:name`, ({ params }) => {
    const machine = machines.find((m) => m.name === params.name)
    if (!machine) return HttpResponse.json({ error: 'not found' }, { status: 404 })
    return HttpResponse.json(machine)
  }),

  http.get(`${API_BASE}/machines/:name/hardware`, ({ params }) => {
    const machine = machines.find((m) => m.name === params.name)
    if (!machine) return HttpResponse.json({ error: 'not found' }, { status: 404 })
    return HttpResponse.json({
      name: `${machine.name}-inventory`,
      machineName: machine.name,
      cpu: { model: machine.arch === 'arm64' ? 'ARM Cortex-A76' : 'Intel Core i7-12700', cores: 12, threads: 20, arch: machine.arch, mhz: '3600' },
      memory: { totalMB: 65536, slots: 4 },
      disks: [{ name: 'nvme0n1', sizeMB: 1_000_000, type: 'nvme', model: 'Samsung SSD 980' }],
      nics: [{ name: 'eno1', mac: machine.mac, speed: '1G', state: 'up' }],
      bios: { vendor: 'American Megatrends', version: '1.0.0', date: '2025-01-10' }
    })
  }),

  http.post(`${API_BASE}/machines/:name/actions/redeploy`, ({ params }) => {
    const machine = machines.find((m) => m.name === params.name)
    if (!machine) return HttpResponse.json({ error: 'not found' }, { status: 404 })
    return HttpResponse.json({
      ...machine,
      phase: 'Provisioning',
      provision: {
        startedAt: new Date().toISOString(),
        trigger: 'redeploy',
        requestedBy: 'admin',
        message: 'provisioning started'
      }
    }, { status: 202 })
  }),

  http.post(`${API_BASE}/machines/:name/actions/power-on`, () => {
    return HttpResponse.json({ status: 'accepted' })
  }),

  http.post(`${API_BASE}/machines/:name/actions/power-off`, () => {
    return HttpResponse.json({ status: 'accepted' })
  }),

  http.patch(`${API_BASE}/machines/:name/settings`, ({ params }) => {
    const machine = machines.find((m) => m.name === params.name)
    if (!machine) return HttpResponse.json({ error: 'not found' }, { status: 404 })
    return HttpResponse.json(machine)
  }),

  http.delete(`${API_BASE}/machines/:name`, () => {
    return new HttpResponse(null, { status: 204 })
  }),

  http.get(`${API_BASE}/subnets`, () => {
    return HttpResponse.json({ items: subnets })
  }),

  http.get(`${API_BASE}/subnets/:name`, ({ params }) => {
    const subnet = subnets.find((s) => s.name === params.name)
    if (!subnet) return HttpResponse.json({ error: 'not found' }, { status: 404 })
    return HttpResponse.json(subnet)
  }),

  http.post(`${API_BASE}/subnets`, async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>
    return HttpResponse.json({
      ...body,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString()
    }, { status: 201 })
  }),

  http.put(`${API_BASE}/subnets/:name`, ({ params }) => {
    const subnet = subnets.find((s) => s.name === params.name)
    if (!subnet) return HttpResponse.json({ error: 'not found' }, { status: 404 })
    return HttpResponse.json(subnet)
  }),

  http.delete(`${API_BASE}/subnets/:name`, () => {
    return new HttpResponse(null, { status: 204 })
  }),

  http.get(`${API_BASE}/audit-events`, () => {
    return HttpResponse.json({ items: auditEvents })
  }),

  http.get(`${API_BASE}/ssh-keys`, () => {
    return HttpResponse.json({ items: [] })
  }),

  http.get(`${API_BASE}/hypervisors`, () => {
    return HttpResponse.json({ items: [] })
  }),

  http.get(`${API_BASE}/virtual-machines`, () => {
    return HttpResponse.json({ items: [] })
  }),

  http.get(`${API_BASE}/cloud-init-templates`, () => {
    return HttpResponse.json({ items: [] })
  }),

  http.get(`${API_BASE}/os-images`, () => {
    return HttpResponse.json({ items: [] })
  }),

  http.get(`${API_BASE}/boot-environments`, () => {
    return HttpResponse.json({ items: [] })
  }),

  http.get(`${API_BASE}/dhcp-leases`, () => {
    return HttpResponse.json({ items: [] })
  }),

  http.get(`${API_BASE}/system-info`, () => {
    return HttpResponse.json({
      hostname: 'gomi-mock',
      os: 'darwin',
      arch: 'arm64',
      goVersion: 'go1.24',
      cpuCount: 10,
      goroutines: 42,
      uptime: 3600,
      memoryUsedMB: 512
    })
  }),

  http.get(`${API_BASE}/os-catalog`, () => {
    return HttpResponse.json({
      items: [
        {
          entry: {
            name: 'ubuntu-22.04-amd64-baremetal',
            osFamily: 'ubuntu',
            osVersion: '22.04',
            arch: 'amd64',
            format: 'squashfs',
            sourceFormat: 'squashfs',
            variant: 'baremetal',
            url: 'https://github.com/sugaf1204/gomi/releases/latest/download/ubuntu-22.04-amd64-baremetal.rootfs.squashfs',
            bootEnvironment: 'ubuntu-minimal-cloud-amd64'
          },
          installed: false,
          installing: false,
          osImageReady: false,
          bootEnvironment: { name: 'ubuntu-minimal-cloud-amd64', phase: 'missing', updatedAt: new Date().toISOString() }
        },
        {
          entry: {
            name: 'debian-13-amd64-cloud',
            osFamily: 'debian',
            osVersion: '13',
            arch: 'amd64',
            format: 'qcow2',
            sourceFormat: 'qcow2',
            variant: 'cloud',
            url: 'https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2',
            bootEnvironment: 'ubuntu-minimal-cloud-amd64'
          },
          installed: false,
          installing: false,
          osImageReady: false,
          bootEnvironment: { name: 'ubuntu-minimal-cloud-amd64', phase: 'missing', updatedAt: new Date().toISOString() }
        }
      ]
    })
  }),

  http.post(`${API_BASE}/os-catalog/:name/install`, ({ params }) => {
    const name = String(params.name)
    const isUbuntu = name.startsWith('ubuntu-')
    const isDebianCloud = name === 'debian-13-amd64-cloud'
    const isBareMetal = name.endsWith('-baremetal')
    const format = isDebianCloud ? 'qcow2' : isBareMetal ? 'squashfs' : 'raw'
    const sourceFormat = format
    const sourceCompression = isBareMetal || isDebianCloud ? undefined : 'zstd'
    const suffix = isDebianCloud ? '.qcow2' : isBareMetal ? '.rootfs.squashfs' : '.raw.zst'
    const imageURL = isDebianCloud
      ? 'https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2'
      : `https://github.com/sugaf1204/gomi/releases/latest/download/${name}${suffix}`
    return HttpResponse.json({
      entry: {
        name,
        osFamily: isUbuntu ? 'ubuntu' : 'debian',
        osVersion: isUbuntu ? (name.includes('22.04') ? '22.04' : '24.04') : '13',
        arch: 'amd64',
        format,
        sourceFormat,
        sourceCompression,
        variant: isBareMetal ? 'baremetal' : 'cloud',
        url: imageURL,
        bootEnvironment: 'ubuntu-minimal-cloud-amd64'
      },
      installed: false,
      installing: true,
      osImageReady: false,
      bootEnvironment: { name: 'ubuntu-minimal-cloud-amd64', phase: 'building', updatedAt: new Date().toISOString() }
    }, { status: 202 })
  })
]
