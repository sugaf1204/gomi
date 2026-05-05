import { formatDate } from '../../lib/formatters'
import type { SystemInfo } from '../../types'

export type OverviewViewProps = {
  lastSyncedAt: string
  systemInfo: SystemInfo | null
  machineCount: number
  subnetCount: number
  machineStats: {
    ready: number
    provisioning: number
    attention: number
  }
  hypervisorCount: number
  vmCount: number
  vmStats: {
    running: number
    stopped: number
    error: number
  }
}

function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h ${minutes}m`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

export function OverviewView({
  lastSyncedAt,
  systemInfo,
  machineCount,
  subnetCount,
  machineStats,
  hypervisorCount,
  vmCount,
  vmStats
}: OverviewViewProps) {
  return (
    <section className="min-h-0 grid content-start gap-[0.95rem]">
      <div className="flex items-baseline gap-[1.2rem] text-[0.84rem] text-ink-soft">
        <span>Last sync: <strong className="text-ink">{formatDate(lastSyncedAt)}</strong></span>
      </div>

      {systemInfo && (
        <section className="grid grid-cols-2 lg:grid-cols-3 gap-x-[1.2rem] gap-y-[0.5rem] bg-transparent border-0 border-t border-line pt-[0.85rem]">
          <p className="m-0 col-span-full text-ink-soft text-[0.78rem] uppercase tracking-[0.06em]">Server</p>
          <p className="m-0 text-[0.82rem]"><span className="text-ink-soft">Hostname </span><strong>{systemInfo.hostname}</strong></p>
          <p className="m-0 text-[0.82rem]"><span className="text-ink-soft">OS / Arch </span><strong>{systemInfo.os}/{systemInfo.arch}</strong></p>
          <p className="m-0 text-[0.82rem]"><span className="text-ink-soft">Go </span><strong>{systemInfo.goVersion}</strong></p>
          <p className="m-0 text-[0.82rem]"><span className="text-ink-soft">CPUs </span><strong>{systemInfo.cpuCount}</strong></p>
          <p className="m-0 text-[0.82rem]"><span className="text-ink-soft">Memory </span><strong>{systemInfo.memoryUsedMB} MB</strong></p>
          <p className="m-0 text-[0.82rem]"><span className="text-ink-soft">Uptime </span><strong>{formatUptime(systemInfo.uptime)}</strong></p>
          <p className="m-0 text-[0.82rem]"><span className="text-ink-soft">Goroutines </span><strong>{systemInfo.goroutines}</strong></p>
        </section>
      )}

      <section className="grid grid-cols-2 lg:grid-cols-4 gap-[0.85rem]">
        <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
          <p className="m-0 text-ink-soft text-[0.78rem] uppercase tracking-[0.06em]">Machines</p>
          <p className="m-0 text-[1.6rem] font-semibold mt-[0.2rem]">{machineCount}</p>
          <p className="m-0 text-ink-soft text-[0.78rem] mt-[0.3rem]">
            {machineStats.ready} ready - {machineStats.provisioning} provisioning - {machineStats.attention} attention
          </p>
        </article>

        <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
          <p className="m-0 text-ink-soft text-[0.78rem] uppercase tracking-[0.06em]">Virtual Machines</p>
          <p className="m-0 text-[1.6rem] font-semibold mt-[0.2rem]">{vmCount}</p>
          {vmCount > 0 ? (
            <p className="m-0 text-ink-soft text-[0.78rem] mt-[0.3rem]">
              {vmStats.running} running - {vmStats.stopped} stopped - {vmStats.error} error
            </p>
          ) : (
            <p className="m-0 text-ink-soft text-[0.78rem] mt-[0.3rem]">No VMs</p>
          )}
        </article>

        <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
          <p className="m-0 text-ink-soft text-[0.78rem] uppercase tracking-[0.06em]">Hypervisors</p>
          <p className="m-0 text-[1.6rem] font-semibold mt-[0.2rem]">{hypervisorCount}</p>
        </article>

        <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
          <p className="m-0 text-ink-soft text-[0.78rem] uppercase tracking-[0.06em]">Subnets</p>
          <p className="m-0 text-[1.6rem] font-semibold mt-[0.2rem]">{subnetCount}</p>
        </article>
      </section>
    </section>
  )
}
