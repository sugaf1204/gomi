import { http, HttpResponse } from 'msw'
import { auditEvents, machines, subnets } from './fixtures'

const API_BASE = 'http://localhost:8080/api/v1'

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

  http.get(`${API_BASE}/os-catalog`, () => {
    return HttpResponse.json({
      items: [
        {
          entry: {
            name: 'debian-13-amd64',
            osFamily: 'debian',
            osVersion: '13',
            arch: 'amd64',
            format: 'raw',
            sourceFormat: 'raw',
            sourceCompression: 'zstd',
            variant: 'server',
            url: 'https://github.com/sugaf1204/gomi/releases/latest/download/debian-13-amd64.raw.zst',
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
    return HttpResponse.json({
      entry: {
        name: params.name,
        osFamily: 'debian',
        osVersion: '13',
        arch: 'amd64',
        format: 'raw',
        sourceFormat: 'raw',
        sourceCompression: 'zstd',
        variant: 'server',
        url: `https://github.com/sugaf1204/gomi/releases/latest/download/${params.name}.raw.zst`,
        bootEnvironment: 'ubuntu-minimal-cloud-amd64'
      },
      installed: false,
      installing: true,
      osImageReady: false,
      bootEnvironment: { name: 'ubuntu-minimal-cloud-amd64', phase: 'building', updatedAt: new Date().toISOString() }
    }, { status: 202 })
  })
]
