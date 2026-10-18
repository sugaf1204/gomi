import { http, HttpResponse } from 'msw'
import type { ClusterAPIChange, ClusterAPIResource } from '../components/views/cluster-api/types'

const base = 'http://localhost:5392/api/v1/cluster-api'
const defaults = { apiVersion: 'cluster.x-k8s.io/v1beta2', namespace: 'default', generation: 1, resourceVersion: '1', paused: false, deleting: false, topology: false, autoscaled: false }
const resources: ClusterAPIResource[] = [
  { ...defaults, resource: 'clusters', name: 'gomi-lab', uid: 'mock-cluster', clusterName: 'gomi-lab', infrastructureName: 'gomi-lab', conditions: [{ type: 'Available', status: 'True', observedGeneration: 1, reason: 'Available' }] },
  { ...defaults, apiVersion: 'controlplane.cluster.x-k8s.io/v1beta2', resource: 'kubeadmcontrolplanes', name: 'gomi-lab-control-plane', uid: 'mock-control-plane', clusterName: 'gomi-lab', replicas: 1, readyReplicas: 1, version: 'v1.35.0', conditions: [{ type: 'Available', status: 'True', observedGeneration: 1 }] },
  { ...defaults, resource: 'machinedeployments', name: 'gomi-lab-workers', uid: 'mock-workers', clusterName: 'gomi-lab', replicas: 2, readyReplicas: 1, version: 'v1.35.0', conditions: [{ type: 'Available', status: 'False', reason: 'WaitingForAvailableMachines', message: '1 of 2 workers is available. Waiting for node registration.', observedGeneration: 1 }] },
  { ...defaults, resource: 'machines', name: 'gomi-lab-control-plane-7s5mz', uid: 'mock-cp-machine', clusterName: 'gomi-lab', nodeName: 'gomi-lab-control-plane-7s5mz', version: 'v1.35.0', infrastructureName: 'gomi-lab-control-plane-7s5mz', providerID: 'gomi:///capi-control-plane', conditions: [{ type: 'Ready', status: 'True', observedGeneration: 1 }] },
  { ...defaults, resource: 'machines', name: 'gomi-lab-workers-q8k4p', uid: 'mock-worker-machine', clusterName: 'gomi-lab', nodeName: 'gomi-lab-workers-q8k4p', version: 'v1.35.0', infrastructureName: 'gomi-lab-workers-q8k4p', providerID: 'gomi:///capi-worker-1', conditions: [{ type: 'Ready', status: 'True', observedGeneration: 1 }] },
  { ...defaults, resource: 'machines', name: 'gomi-lab-workers-x2n9h', uid: 'mock-pending-machine', clusterName: 'gomi-lab', version: 'v1.35.0', infrastructureName: 'gomi-lab-workers-x2n9h', conditions: [{ type: 'Ready', status: 'False', reason: 'NodeNotHealthy', message: 'Waiting for a Node with the matching provider ID.', observedGeneration: 1 }] },
]

export const clusterAPIHandlers = [
  http.get(base, () => HttpResponse.json({ configured: true, context: 'gomi-management (demo)', namespace: 'default', observedAt: new Date().toISOString(), resources })),
  http.patch(`${base}/:namespace/:resource/:name`, async ({ params, request }) => {
    const change = await request.json() as ClusterAPIChange
    const resource = resources.find((item) => item.namespace === params.namespace && item.resource === params.resource && item.name === params.name)
    if (!resource) return HttpResponse.json({ error: 'Resource not found' }, { status: 404 })
    if (change.uid !== resource.uid || change.resourceVersion !== resource.resourceVersion) return HttpResponse.json({ error: 'Resource changed. Refresh and review the change again.' }, { status: 409 })
    if (change.replicas !== undefined) resource.replicas = change.replicas
    if (change.paused !== undefined) resource.paused = change.paused
    resource.generation = (resource.generation ?? 1) + 1
    resource.resourceVersion = String(Number(resource.resourceVersion) + 1)
    // Mocks acknowledge desired state only; they do not simulate provisioning.
    return HttpResponse.json(resource)
  }),
]
