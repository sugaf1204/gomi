import { http, HttpResponse } from 'msw'
import { auditEvents, dnsRecords, machines, subnets } from './fixtures'

const API_BASE = 'http://localhost:5392/api/v1'

function apiPattern(path: string) {
  const escapedBase = API_BASE.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`^${escapedBase}${path}$`)
}

function customMethodName(url: string, method: string) {
  const last = new URL(url).pathname.split('/').pop() ?? ''
  return decodeURIComponent(last.replace(`:${method}`, ''))
}

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

  http.post(`${API_BASE}/me/password`, () => {
    return new HttpResponse(null, { status: 204 })
  }),

  http.get(`${API_BASE}/machines`, () => {
    return HttpResponse.json({ machines, totalSize: machines.length })
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

  http.post(apiPattern('/machines/[^/]+:redeploy'), ({ request }) => {
    const machineName = customMethodName(request.url, 'redeploy')
    const machine = machines.find((m) => m.name === machineName)
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

  http.post(apiPattern('/machines/[^/]+:powerOn'), () => {
    return HttpResponse.json({ status: 'accepted' })
  }),

  http.post(apiPattern('/machines/[^/]+:powerOff'), () => {
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
    return HttpResponse.json({ subnets, totalSize: subnets.length })
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
    return HttpResponse.json({ auditEvents, totalSize: auditEvents.length })
  }),

  http.get(`${API_BASE}/ssh-keys`, () => {
    return HttpResponse.json({ sshKeys: [], totalSize: 0 })
  }),

  http.get(`${API_BASE}/hypervisors`, () => {
    return HttpResponse.json({ hypervisors: [], totalSize: 0 })
  }),

  http.get(`${API_BASE}/virtual-machines`, () => {
    return HttpResponse.json({ virtualMachines: [], totalSize: 0 })
  }),

  http.get(`${API_BASE}/cloud-init-templates`, () => {
    return HttpResponse.json({ cloudInitTemplates: [], totalSize: 0 })
  }),

  http.get(`${API_BASE}/os-images`, () => {
    return HttpResponse.json({ osImages: [], totalSize: 0 })
  }),

  http.get(`${API_BASE}/boot-environments`, () => {
    return HttpResponse.json({ bootEnvironments: [], totalSize: 0 })
  }),

  http.get(`${API_BASE}/dhcp-leases`, () => {
    return HttpResponse.json({ dhcpLeases: [], totalSize: 0 })
  }),

  http.get(`${API_BASE}/dns-records`, () => {
    return HttpResponse.json({ dnsRecords, totalSize: dnsRecords.length })
  }),

  http.post(`${API_BASE}/dns-records`, async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>
    return HttpResponse.json({
      ...body,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString()
    }, { status: 201 })
  }),

  http.put(`${API_BASE}/dns-records/:name/:type`, async ({ params, request }) => {
    const body = (await request.json()) as Record<string, unknown>
    return HttpResponse.json({
      ...body,
      name: params.name,
      type: params.type,
      updatedAt: new Date().toISOString()
    })
  }),

  http.delete(`${API_BASE}/dns-records/:name/:type`, () => {
    return new HttpResponse(null, { status: 204 })
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

]
