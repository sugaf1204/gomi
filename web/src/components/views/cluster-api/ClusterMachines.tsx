import { ResourceIssue, ResourceStatus } from './ResourceStatus'
import { resourceKey, type ClusterAPIResource } from './types'

export function ClusterMachines({ machines }: { machines: ClusterAPIResource[] }) {
  if (machines.length === 0) return <p className="text-[12px] text-ink-soft">No Machines reported.</p>
  return <div className="min-w-0 overflow-x-auto">
    <table className="min-w-[540px] table-fixed">
      <caption className="sr-only">Machines in the selected cluster</caption>
      <thead><tr>
        <th className="w-[44%] text-[10.5px] font-mono uppercase tracking-[0.08em]">Machine</th>
        <th className="w-[36%] text-[10.5px] font-mono uppercase tracking-[0.08em]">Node</th>
        <th className="w-[20%] text-[10.5px] font-mono uppercase tracking-[0.08em]">State</th>
      </tr></thead>
      <tbody>{machines.map((machine) => <tr key={resourceKey(machine)} className="align-top cursor-default!">
        <td className="text-[12px]">
          <p className="m-0 font-mono font-medium break-anywhere">{machine.name}</p>
          <p className="m-0 mt-1 font-mono text-[11px] text-ink-mid">{machine.version || 'Version unknown'}</p>
          <details className="mt-2 text-[11px] text-ink-mid">
            <summary className="cursor-pointer w-fit focus-visible:outline-2 focus-visible:outline-brand">Infrastructure details</summary>
            <dl className="grid gap-1 mt-2 mb-0">
              <dt className="text-ink-soft">Infrastructure</dt><dd className="m-0 font-mono break-anywhere">{machine.infrastructureName || 'Not reported'}</dd>
              <dt className="text-ink-soft">Provider ID</dt><dd className="m-0 font-mono break-anywhere">{machine.providerID || 'Not reported'}</dd>
            </dl>
          </details>
        </td>
        <td className="text-[12px] font-mono break-anywhere">{machine.nodeName || <span className="text-warn">Not registered</span>}</td>
        <td><ResourceStatus resource={machine} /></td>
      </tr>)}</tbody>
    </table>
    <div className="mt-4 grid gap-3">
      {machines.filter((machine) => machine.conditions.some((condition) => ['Ready', 'Available'].includes(condition.type) && condition.status !== 'True')).map((machine) => <div key={resourceKey(machine)}>
        <p className="m-0 mb-1 font-mono text-[11px] text-ink-mid break-anywhere">{machine.name}</p>
        <ResourceIssue resource={machine} />
      </div>)}
    </div>
  </div>
}
