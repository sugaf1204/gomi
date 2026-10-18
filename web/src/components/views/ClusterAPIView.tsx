import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../../api'
import { ResourceWorkspace } from '../ui/ResourceWorkspace'
import { ClusterAPIChangeDialog, type PendingChange } from './cluster-api/ClusterAPIChangeDialog'
import { ClusterDetails } from './cluster-api/ClusterDetails'
import { ClusterIndex } from './cluster-api/ClusterIndex'
import { resourceKey, type ClusterAPISnapshot } from './cluster-api/types'

export type ClusterAPIViewProps = { canWrite: boolean }

export function ClusterAPIView({ canWrite }: ClusterAPIViewProps) {
  const [snapshot, setSnapshot] = useState<ClusterAPISnapshot | null>(null)
  const [selected, setSelected] = useState('')
  const [filter, setFilter] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [pending, setPending] = useState<PendingChange | null>(null)
  const [notice, setNotice] = useState('')
  const mounted = useRef(false)
  const inFlight = useRef(false)

  const refresh = useCallback(async () => {
    if (inFlight.current) return
    inFlight.current = true
    setLoading(true)
    try {
      const result = await api.clusterAPI()
      if (mounted.current) { setSnapshot(result); setError('') }
    } catch (err) {
      if (mounted.current) setError(err instanceof Error ? err.message : 'Failed to load Cluster API resources')
    } finally {
      inFlight.current = false
      if (mounted.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    mounted.current = true
    void refresh()
    const timer = window.setInterval(() => { if (document.visibilityState !== 'hidden') void refresh() }, 15000)
    return () => { mounted.current = false; window.clearInterval(timer) }
  }, [refresh])

  const clusters = snapshot?.resources.filter((resource) => resource.resource === 'clusters') ?? []
  const visible = clusters.filter((cluster) => resourceKey(cluster).toLowerCase().includes(filter.toLowerCase()))
  const cluster = visible.find((item) => resourceKey(item) === selected) ?? visible[0]
  const writable = canWrite && !error

  function applied() {
    setPending(null)
    setNotice('Desired state updated. Controllers are reconciling the change; monitor readiness below.')
    void refresh()
  }

  return <div className="h-full min-h-0 flex flex-col">
    {error && <div role="alert" className="shrink-0 border-b border-error-line bg-error-bg text-error px-5 py-3 text-[12px] leading-relaxed">
      {error}{snapshot && <span> · Displaying the last successful observation. Operations are disabled.</span>}
    </div>}
    <div className="min-h-0 flex-1">
      <ResourceWorkspace index={<ClusterIndex clusters={visible} selected={cluster} filter={filter}
        context={snapshot?.context} namespace={snapshot?.namespace} observedAt={snapshot?.configured ? snapshot.observedAt : undefined}
        loading={loading} canWrite={canWrite} onFilter={setFilter} onSelect={setSelected} onRefresh={() => void refresh()} />}>
        <div className="min-h-0 min-w-0 overflow-auto grid content-start gap-[18px] p-[20px_22px] max-sm:p-[14px]">
          {notice && <p role="status" className="m-0 border-l-2 border-brand bg-brand-wash px-3 py-2 text-[12px] leading-relaxed">{notice}</p>}
          {!snapshot && loading && <p role="status" className="text-[12px] text-ink-mid">Loading Cluster API resources…</p>}
          {snapshot && !snapshot.configured && <section className="border-t border-line pt-4 grid gap-3">
            <h2 className="text-[20px]">Connect a management cluster</h2>
            <p className="m-0 max-w-lg text-[13px] leading-relaxed text-ink-mid">Configure the GOMI server with a management-cluster kubeconfig to view clusters and manage worker counts.</p>
            <code className="text-[12px] break-anywhere text-ink-soft">cluster_api.kubeconfig · cluster_api.context · cluster_api.namespace</code>
          </section>}
          {snapshot?.configured && !cluster && <section className="border-t border-line pt-4">
            <h2 className="text-[20px]">{clusters.length ? 'No matching clusters' : 'No clusters found'}</h2>
            <p className="text-[13px] text-ink-mid">{clusters.length ? 'Change the filter to select a cluster.' : 'No Cluster API clusters were found in the configured scope.'}</p>
          </section>}
          {cluster && snapshot && <ClusterDetails key={resourceKey(cluster)} cluster={cluster} resources={snapshot.resources}
            canWrite={Boolean(writable)} onChange={setPending} />}
        </div>
      </ResourceWorkspace>
    </div>
    {pending && <ClusterAPIChangeDialog pending={pending} context={snapshot?.context} unavailable={!writable}
      onClose={() => setPending(null)} onApplied={applied} />}
  </div>
}
