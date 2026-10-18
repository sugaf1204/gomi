export type ClusterAPIResource = {
  resource: 'clusters' | 'machinedeployments' | 'machines' | 'kubeadmcontrolplanes'
  apiVersion: string
  namespace: string
  name: string
  uid: string
  resourceVersion: string
  generation?: number
  clusterName?: string
  phase?: string
  version?: string
  replicas?: number
  readyReplicas?: number
  paused: boolean
  deleting: boolean
  topology: boolean
  autoscaled: boolean
  nodeName?: string
  providerID?: string
  infrastructureName?: string
  conditions: { type: string; status: string; reason?: string; message?: string; observedGeneration?: number }[]
}

export type ClusterAPISnapshot = {
  configured: boolean
  context?: string
  namespace?: string
  observedAt?: string
  resources: ClusterAPIResource[]
}

export type ClusterAPIChange = Pick<ClusterAPIResource, 'apiVersion' | 'uid' | 'resourceVersion'> & {
  replicas?: number
  paused?: boolean
}

export function resourceKey(resource: ClusterAPIResource) {
  return `${resource.namespace}/${resource.name}`
}

export function scaleRestriction(cluster: ClusterAPIResource, group: ClusterAPIResource): string | undefined {
  if (cluster.deleting || group.deleting) return 'Resource is being deleted.'
  if (cluster.paused || group.paused) return 'Resume reconciliation before scaling.'
  if (cluster.topology) return 'Replicas are managed by Cluster topology.'
  if (group.autoscaled) return 'Replicas are managed by an autoscaler.'
  return undefined
}
