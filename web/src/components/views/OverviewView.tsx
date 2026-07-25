import { useMemo } from 'react'
import type { View } from '../../app-types'
import { deriveDeployTimeline } from '../../lib/deploy-timeline'
import { DeployCard } from '../deploy/DeployCard'
import { FleetBar, type FleetSlice } from './overview/FleetBar'
import { NeedsYou, collectAttention } from './overview/NeedsYou'
import type { Machine, SystemInfo, VirtualMachine } from '../../types'

export type OverviewViewProps = {
  systemInfo: SystemInfo | null
  machines: Machine[]
  virtualMachines: VirtualMachine[]
  machineStats: {
    ready: number
    provisioning: number
    attention: number
  }
  vmStats: {
    running: number
    stopped: number
    error: number
    missing: number
  }
  onJump: (view: View, target: string) => void
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
  systemInfo,
  machines,
  virtualMachines,
  machineStats,
  vmStats,
  onJump,
}: OverviewViewProps) {
  const attention = useMemo(() => collectAttention(machines, virtualMachines), [machines, virtualMachines])

  // The machine currently deploying gets the in-flight card; the 5s poll
  // re-derives its timeline so the tone advances without anything moving.
  const deploying = machines.find((machine) => machine.phase.toLowerCase() === 'provisioning')
  const timeline = useMemo(
    () => (deploying ? deriveDeployTimeline(deploying.provision, Date.now()) : null),
    [deploying]
  )

  const metalSlices: FleetSlice[] = [
    { key: 'ready', count: machineStats.ready, label: 'READY', fill: 'bg-ok-bg', border: 'border-ok-line', text: 'text-ok' },
    { key: 'provisioning', count: machineStats.provisioning, label: 'PROVISIONING', fill: 'bg-warn-bg', border: 'border-warn-line', text: 'text-warn' },
    { key: 'attention', count: machineStats.attention, label: 'ATTENTION', fill: 'bg-error-bg', border: 'border-error-line', text: 'text-error' },
  ]

  const vmSlices: FleetSlice[] = [
    { key: 'running', count: vmStats.running, label: 'RUNNING', fill: 'bg-ok-bg', border: 'border-ok-line', text: 'text-ok' },
    { key: 'stopped', count: vmStats.stopped, label: 'STOPPED', fill: 'bg-neutral-bg', border: 'border-line', text: 'text-ink-soft' },
    { key: 'error', count: vmStats.error + vmStats.missing, label: 'ATTENTION', fill: 'bg-error-bg', border: 'border-error-line', text: 'text-error' },
  ]

  return (
    <section className="min-h-0 grid content-start gap-[22px]">
      <div className="grid gap-[18px]">
        <FleetBar
          heading="BARE METAL"
          total={machines.length}
          summary={`${machineStats.ready} ready · ${machineStats.provisioning} provisioning · ${machineStats.attention} attention`}
          slices={metalSlices}
        />
        <FleetBar
          heading="VIRTUAL"
          total={virtualMachines.length}
          summary={`${vmStats.running} running · ${vmStats.stopped} stopped · ${vmStats.error + vmStats.missing} attention`}
          slices={vmSlices}
        />
      </div>

      <div className="grid grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)] gap-[22px] max-md:grid-cols-1">
        <section className="grid content-start gap-2">
          <p className="m-0 font-mono font-semibold text-[10px] tracking-[0.16em] text-ink-soft">IN FLIGHT</p>
          {timeline && deploying ? (
            <DeployCard
              timeline={timeline}
              attemptId={deploying.provision?.attemptId}
              lastSignal={`${deploying.name} · ${timeline.events.at(-1)?.name ?? 'no signal yet'}`}
            />
          ) : (
            <p className="m-0 font-mono text-[10.5px] text-ink-soft">no deploy in flight</p>
          )}
        </section>

        <NeedsYou items={attention} services={serviceChips(systemInfo)} onJump={onJump} />
      </div>

      {systemInfo && <ServerFooter systemInfo={systemInfo} />}
    </section>
  )
}

function serviceChips(systemInfo: SystemInfo | null): { label: string; tone: 'ok' | 'warning' }[] {
  if (!systemInfo) return []
  return [
    { label: 'dhcp ready', tone: 'ok' },
    { label: 'tftp ready', tone: 'ok' },
    { label: 'dns ready', tone: 'ok' },
  ]
}

/** Server facts move out of the top of the page to the bottom. */
function ServerFooter({ systemInfo }: { systemInfo: SystemInfo }) {
  const facts: { caption: string; value: string }[] = [
    { caption: 'hostname', value: systemInfo.hostname },
    { caption: 'os / arch', value: `${systemInfo.os}/${systemInfo.arch}` },
    { caption: 'runtime', value: systemInfo.goVersion },
    { caption: 'cpus', value: String(systemInfo.cpuCount) },
    { caption: 'memory', value: `${systemInfo.memoryUsedMB} MB` },
    { caption: 'uptime', value: formatUptime(systemInfo.uptime) },
    { caption: 'goroutines', value: String(systemInfo.goroutines) },
  ]

  return (
    <footer className="flex flex-wrap gap-x-6 gap-y-3 border-t border-line bg-panel-2 pt-3">
      {facts.map((fact) => (
        <div key={fact.caption}>
          <p className="m-0 font-mono text-[12px] text-ink">{fact.value}</p>
          <p className="m-0 font-mono text-[10.5px] text-ink-soft">{fact.caption}</p>
        </div>
      ))}
    </footer>
  )
}
