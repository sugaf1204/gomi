import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within, waitFor, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { api } from '../../api'
import { ClusterAPIView } from './ClusterAPIView'
import type { ClusterAPIResource, ClusterAPISnapshot } from './cluster-api/types'

const cluster: ClusterAPIResource = { resource: 'clusters', apiVersion: 'cluster.x-k8s.io/v1beta2', namespace: 'lab', name: 'demo', uid: 'cluster-uid', resourceVersion: '7', clusterName: 'demo', paused: false, deleting: false, topology: false, autoscaled: false, conditions: [{ type: 'Available', status: 'True' }] }
const worker: ClusterAPIResource = { ...cluster, resource: 'machinedeployments', name: 'workers', uid: 'workers-uid', replicas: 2, readyReplicas: 1, version: 'v1.35.0', conditions: [{ type: 'Available', status: 'False', reason: 'ScalingUp', message: 'Waiting for node registration' }] }
const snapshot: ClusterAPISnapshot = { configured: true, context: 'management-test', namespace: 'lab', resources: [cluster, worker] }

afterEach(() => { cleanup(); vi.restoreAllMocks() })

function load(value = snapshot) { vi.spyOn(api, 'clusterAPI').mockResolvedValue(value) }

describe('Cluster API workspace', () => {
  it('explains how to configure a disconnected management cluster', async () => {
    load({ configured: false, resources: [] })
    render(<ClusterAPIView canWrite />)
    expect(await screen.findByText('Connect a management cluster')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Pause reconciliation' })).not.toBeInTheDocument()
  })
  it('shows conditions, namespace scope and readonly access', async () => {
    load()
    render(<ClusterAPIView canWrite={false} />)
    expect(await screen.findByText('management-test')).toBeInTheDocument()
    expect(screen.getByText('Waiting for node registration')).toBeInTheDocument()
    expect(screen.getByText(/Read-only access/)).toBeInTheDocument()
    expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
  })
  it('requires a named confirmation and sends the displayed resource version when scaling to zero', async () => {
    load()
    const change = vi.spyOn(api, 'changeClusterAPI').mockResolvedValue({ ...worker, replicas: 0 })
    const user = userEvent.setup()
    render(<ClusterAPIView canWrite />)
    const input = await screen.findByRole('spinbutton')
    await user.clear(input)
    await user.type(input, '0')
    await user.click(screen.getByRole('button', { name: 'Review change' }))
    const dialog = within(screen.getByRole('dialog'))
    expect(dialog.getByText(/Scaling down drains and deletes/)).toBeInTheDocument()
    expect(dialog.getByRole('button', { name: 'Apply change' })).toBeDisabled()
    await user.type(dialog.getByLabelText('Confirm resource name'), 'lab/workers')
    await user.click(dialog.getByRole('button', { name: 'Apply change' }))
    await waitFor(() => expect(change).toHaveBeenCalledWith(worker, { apiVersion: worker.apiVersion, uid: 'workers-uid', resourceVersion: '7', replicas: 0 }))
    expect(await screen.findByRole('status')).toHaveTextContent('Desired state updated')
  })
  it('keeps failures in the confirmation dialog for review', async () => {
    load()
    vi.spyOn(api, 'changeClusterAPI').mockRejectedValue(new Error('Resource changed. Refresh and review the change again.'))
    const user = userEvent.setup()
    render(<ClusterAPIView canWrite />)
    await user.click(await screen.findByRole('button', { name: 'Pause reconciliation' }))
    await user.type(screen.getByLabelText('Confirm resource name'), 'lab/demo')
    await user.click(screen.getByRole('button', { name: 'Apply change' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Resource changed')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })
  it.each(['topology', 'autoscaled', 'paused'] as const)('disables scaling when %s is managed elsewhere', async (reason) => {
    load({ ...snapshot, resources: [{ ...cluster, topology: reason === 'topology', paused: reason === 'paused' }, { ...worker, autoscaled: reason === 'autoscaled' }] })
    render(<ClusterAPIView canWrite />)
    expect(await screen.findByRole('spinbutton')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Review change' })).toBeDisabled()
  })
  it('keeps identical cluster names in different namespaces separate', async () => {
    load({ ...snapshot, resources: [cluster, worker, { ...cluster, namespace: 'other' }] })
    const user = userEvent.setup()
    render(<ClusterAPIView canWrite />)
    const nav = await screen.findByRole('navigation', { name: 'Clusters' })
    await user.click(within(nav).getByRole('button', { name: /other/ }))
    expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
    expect(screen.getByText('No MachineDeployments reported.')).toBeInTheDocument()
  })
  it('disables actions on stale data after refresh fails', async () => {
    const fetch = vi.spyOn(api, 'clusterAPI').mockResolvedValueOnce(snapshot).mockRejectedValue(new Error('Management request failed'))
    const user = userEvent.setup()
    render(<ClusterAPIView canWrite />)
    await screen.findByRole('spinbutton')
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('last successful observation')
    expect(screen.queryByRole('button', { name: 'Pause reconciliation' })).not.toBeInTheDocument()
    expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
    expect(fetch).toHaveBeenCalledTimes(2)
  })
})

it('shows reconciliation pending instead of treating an old condition as current', async () => {
  load({ ...snapshot, resources: [{ ...cluster, generation: 2, conditions: [{ type: 'Available', status: 'True', observedGeneration: 1 }] }] })
  render(<ClusterAPIView canWrite={false} />)
  expect((await screen.findAllByText('Reconciling')).length).toBe(2)
  expect(within(screen.getByRole('navigation', { name: 'Clusters' })).queryByText('Available: True')).not.toBeInTheDocument()
})

it('separates machine inventory and raw conditions from the operational overview', async () => {
  const machine: ClusterAPIResource = { ...cluster, resource: 'machines', name: 'node-a', nodeName: 'k8s-node-a', providerID: 'gomi:///node-a', infrastructureName: 'infra-a', version: 'v1.35.0' }
  load({ ...snapshot, resources: [cluster, worker, machine] })
  const user = userEvent.setup()
  render(<ClusterAPIView canWrite />)
  await screen.findByRole('heading', { name: 'demo' })
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  expect(screen.getByText('Waiting for node registration')).toBeVisible()
  expect(screen.queryByText('gomi:///node-a')).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Machines (1)' }))
  const table = screen.getByRole('table', { name: 'Machines in the selected cluster' })
  expect(within(table).getByText('k8s-node-a')).toBeVisible()
  expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
  await user.click(screen.getByText('Infrastructure details'))
  expect(within(table).getByText('gomi:///node-a')).toBeVisible()

  await user.click(screen.getByRole('button', { name: 'Conditions' }))
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  expect(screen.getByText('Waiting for node registration')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Overview' }))
  expect(screen.getByRole('spinbutton')).toBeVisible()
})

it('cancels a reviewed operation with Escape without sending a change', async () => {
  load()
  const change = vi.spyOn(api, 'changeClusterAPI')
  const user = userEvent.setup()
  render(<ClusterAPIView canWrite />)
  await user.click(await screen.findByRole('button', { name: 'Pause reconciliation' }))
  expect(screen.getByRole('dialog')).toBeInTheDocument()
  await user.keyboard('{Escape}')
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(change).not.toHaveBeenCalled()
})
