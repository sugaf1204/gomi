import clsx from 'clsx'
import { ResourceStatus } from './ResourceStatus'
import { resourceKey, type ClusterAPIResource } from './types'

type Props = {
  clusters: ClusterAPIResource[]
  selected?: ClusterAPIResource
  filter: string
  context?: string
  namespace?: string
  observedAt?: string
  loading: boolean
  canWrite: boolean
  onFilter: (value: string) => void
  onSelect: (key: string) => void
  onRefresh: () => void
}

export function ClusterIndex(props: Props) {
  return <aside className="min-h-0 grid grid-rows-[auto_auto_minmax(0,1fr)_auto] border-r border-line bg-panel-2">
    <div className="pt-3 px-[14px] pb-3 border-b border-line">
      <p className="m-0 font-mono text-[10px] uppercase tracking-[0.14em] text-ink-soft">Management cluster</p>
      <p className="m-0 mt-1 text-[12px] font-mono font-medium break-anywhere">{props.context || 'Not connected'}</p>
      <p className="m-0 mt-1 text-[11px] text-ink-soft">Namespace: {props.namespace || 'All namespaces'}{!props.canWrite && ' · Read-only access'}</p>
    </div>
    <div className="flex items-center gap-2 p-[12px_14px]">
      <input aria-label="Find a cluster" type="search" placeholder="filter namespace, cluster"
        value={props.filter} onChange={(event) => props.onFilter(event.target.value)}
        className="min-w-0 flex-1 py-[6px] px-2 font-mono text-[11.5px]" />
      <button onClick={props.onRefresh} disabled={props.loading} className="shrink-0 py-[6px] px-[10px] text-[11.5px]">
        {props.loading ? 'Refreshing…' : 'Refresh'}
      </button>
    </div>
    <nav aria-label="Clusters" className="min-h-0 overflow-auto max-md:max-h-[260px]">
      <div className="flex justify-between py-2 px-[14px] bg-panel-3 border-y border-line font-mono text-[10.5px] text-ink-soft">
        <span className="uppercase tracking-[0.1em]">Clusters</span><span>{props.clusters.length}</span>
      </div>
      {props.clusters.map((cluster) => {
        const selected = cluster === props.selected
        return <button key={resourceKey(cluster)} aria-current={selected ? 'page' : undefined}
          onClick={() => props.onSelect(resourceKey(cluster))}
          className={clsx('w-full min-w-0 grid grid-cols-[3px_minmax(0,1fr)] text-left border-0 border-b border-line-soft rounded-none p-0 shadow-none hover:transform-none! hover:shadow-none! focus-visible:outline-2 focus-visible:outline-brand focus-visible:outline-offset-[-2px]', selected ? 'bg-brand-wash' : 'bg-panel')}>
          <span aria-hidden="true" className={selected ? 'bg-brand' : 'bg-transparent'} />
          <span className="min-w-0 px-[14px] py-[10px] grid gap-1.5">
            <span className="flex items-start justify-between gap-2">
              <span className="font-mono font-medium text-[12.5px] break-anywhere">{cluster.name}</span>
              <ResourceStatus resource={cluster} />
            </span>
            <span className="font-mono text-[11px] text-ink-mid break-anywhere">{cluster.namespace}</span>
          </span>
        </button>
      })}
      {!props.clusters.length && <p className="m-0 p-5 text-center text-[12px] text-ink-soft">{props.loading ? 'Loading clusters…' : 'No matching clusters'}</p>}
    </nav>
    <p className="m-0 px-[14px] py-[10px] border-t border-line font-mono text-[10.5px] text-ink-soft">
      {props.observedAt ? `Observed ${new Date(props.observedAt).toLocaleTimeString()}` : 'No observation yet'} · refresh 15s
    </p>
  </aside>
}
