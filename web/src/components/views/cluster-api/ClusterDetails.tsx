import { useState, type ReactNode } from 'react'
import { ResourceSections } from '../../ui/ResourceSections'
import type { PendingChange } from './ClusterAPIChangeDialog'
import { ClusterMachines } from './ClusterMachines'
import { GroupSummary } from './GroupSummary'
import { ResourceConditions, ResourceIssue, ResourceStatus } from './ResourceStatus'
import { WorkerGroup } from './WorkerGroup'
import { resourceKey, type ClusterAPIResource } from './types'

type Props = {
  cluster: ClusterAPIResource
  resources: ClusterAPIResource[]
  canWrite: boolean
  onChange: (change: PendingChange) => void
}

type Section = 'overview' | 'machines' | 'conditions'
const kindLabel = { clusters: 'Cluster', kubeadmcontrolplanes: 'Control plane', machinedeployments: 'Worker group', machines: 'Machine' }

function ResourceGroup({ title, count, children }: { title: string; count: number; children: ReactNode }) {
  return <section className="border-t border-line">
    <h3 className="flex items-baseline gap-2 pt-3 text-[10.5px] font-mono font-semibold uppercase tracking-[0.12em] text-ink-mid">
      {title}<span className="text-ink-soft font-normal">{count}</span>
    </h3>
    {children}
  </section>
}

export function ClusterDetails({ cluster, resources, canWrite, onChange }: Props) {
  const [section, setSection] = useState<Section>('overview')
  const children = resources.filter((resource) => resource.resource !== 'clusters'
    && resource.namespace === cluster.namespace && resource.clusterName === cluster.name)
  const workers = children.filter((resource) => resource.resource === 'machinedeployments')
  const controlPlanes = children.filter((resource) => resource.resource === 'kubeadmcontrolplanes')
  const machines = children.filter((resource) => resource.resource === 'machines')

  return <section className="grid gap-[18px] min-w-0" aria-label="Cluster details">
    <header className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-4 max-sm:grid-cols-1">
      <div className="min-w-0">
        <p className="m-0 font-mono text-[10px] uppercase tracking-[0.14em] text-ink-soft">CLUSTER · {cluster.namespace}</p>
        <h2 className="mt-2 font-mono font-medium text-[30px] tracking-[-0.02em] break-anywhere">{cluster.name}</h2>
        <p className="m-0 mt-1 text-[12px] text-ink-mid">{cluster.topology ? 'Managed topology' : 'Standalone resources'} · {machines.length} machine{machines.length === 1 ? '' : 's'}</p>
      </div>
      <div className="flex flex-col items-end gap-[0.55rem] max-sm:items-start">
        <ResourceStatus resource={cluster} />
        {canWrite && <button className="py-[7px] px-[11px] text-[12px] font-medium" disabled={cluster.deleting}
          onClick={() => onChange({ resource: cluster, paused: !cluster.paused })}>
          {cluster.paused ? 'Resume reconciliation' : 'Pause reconciliation'}
        </button>}
      </div>
    </header>
    <ResourceSections<Section> label="Cluster sections" active={section} onChange={setSection} items={[
      { key: 'overview', label: 'Overview' },
      { key: 'machines', label: `Machines (${machines.length})` },
      { key: 'conditions', label: 'Conditions' },
    ]} />
    {section === 'overview' && <div className="grid gap-[18px]">
      <ResourceIssue resource={cluster} />
      <dl className="m-0 grid grid-cols-[104px_minmax(0,1fr)] gap-x-[10px] gap-y-[7px]">
        <dt className="font-mono text-[10.5px] text-ink-soft">NAMESPACE</dt><dd className="m-0 font-mono text-[12px] break-anywhere">{cluster.namespace}</dd>
        <dt className="font-mono text-[10.5px] text-ink-soft">INFRASTRUCTURE</dt><dd className="m-0 font-mono text-[12px] break-anywhere">{cluster.infrastructureName || 'Not reported'}</dd>
      </dl>
      <ResourceGroup title="Control plane" count={controlPlanes.length}>
        {controlPlanes.map((plane) => <GroupSummary key={resourceKey(plane)} resource={plane} />)}
        {controlPlanes.length === 0 && <p className="text-[12px] text-ink-mid">No KubeadmControlPlane resources reported. Other control-plane providers are not shown.</p>}
      </ResourceGroup>
      <ResourceGroup title="Worker groups" count={workers.length}>
        {workers.map((group) => <WorkerGroup key={`${resourceKey(group)}/${group.replicas}`}
          cluster={cluster} group={group} canWrite={canWrite} onChange={onChange} />)}
        {workers.length === 0 && <p className="text-[12px] text-ink-mid">No MachineDeployments reported.</p>}
      </ResourceGroup>
    </div>}
    {section === 'machines' && <ClusterMachines machines={machines} />}
    {section === 'conditions' && <div className="grid gap-5">
      {[cluster, ...children].map((resource) => <section key={`${resource.resource}/${resourceKey(resource)}`} className="border-t border-line pt-3">
        <p className="m-0 mb-1 text-[10px] font-mono uppercase tracking-[0.1em] text-ink-soft">{kindLabel[resource.resource]}</p>
        <h3 className="font-mono text-[12.5px] break-anywhere">{resource.name}</h3>
        <ResourceConditions resource={resource} />
      </section>)}
    </div>}
  </section>
}
