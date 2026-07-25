import clsx from 'clsx'
import type { View } from '../../../app-types'
import type { Machine, VirtualMachine } from '../../../types'

export type AttentionItem = {
  key: string
  name: string
  reason: string
  action: string
  tone: 'error' | 'warning' | 'neutral'
  view: View
  target: string
}

type Props = {
  items: AttentionItem[]
  services: { label: string; tone: 'ok' | 'warning' }[]
  onJump: (view: View, target: string) => void
}

const toneClass = {
  error: { row: 'border-error-line bg-error-bg', name: 'text-error' },
  warning: { row: 'border-warn-line bg-warn-bg', name: 'text-warn' },
  neutral: { row: 'border-line bg-panel', name: 'text-ink' },
} as const

/** Everything asking for an operator decision, one row each. */
export function NeedsYou({ items, services, onJump }: Props) {
  return (
    <section className="grid content-start gap-2">
      <p className="m-0 font-mono font-semibold text-[10px] tracking-[0.16em] text-ink-soft">NEEDS YOU</p>

      {items.length === 0 && (
        <p className="m-0 font-mono text-[10.5px] text-ink-soft">nothing needs attention</p>
      )}

      {items.map((item) => (
        <button
          key={item.key}
          onClick={() => onJump(item.view, item.target)}
          className={clsx(
            'w-full text-left flex items-baseline gap-2 border shadow-none rounded-none py-[10px] px-[11px]',
            'hover:transform-none! hover:shadow-none!',
            toneClass[item.tone].row
          )}
        >
          <span className={clsx('font-mono font-medium text-[11.5px] truncate', toneClass[item.tone].name)}>{item.name}</span>
          <span className="font-mono text-[10.5px] text-ink-soft truncate">{item.reason}</span>
          <span className="ml-auto shrink-0 font-mono text-[10.5px] text-ink-soft">{item.action} →</span>
        </button>
      ))}

      <p className="m-0 mt-2 font-mono font-semibold text-[10px] tracking-[0.16em] text-ink-soft">SERVICES</p>
      <div className="flex flex-wrap gap-[6px]">
        {services.map((service) => (
          <span
            key={service.label}
            className={clsx(
              'font-mono text-[10.5px] py-[5px] px-[7px] border',
              service.tone === 'ok' ? 'border-line bg-panel text-ink-soft' : 'border-warn-line bg-warn-bg text-warn'
            )}
          >
            {service.label}
          </span>
        ))}
      </div>
    </section>
  )
}

/** Machines and VMs whose state needs an operator, worst first. */
export function collectAttention(machines: Machine[], virtualMachines: VirtualMachine[]): AttentionItem[] {
  const items: AttentionItem[] = []

  for (const machine of machines) {
    const phase = machine.phase.toLowerCase()
    if (phase === 'error' || phase === 'failed') {
      items.push({
        key: `machine:${machine.name}`, name: machine.name, view: 'machines', target: machine.name,
        reason: machine.lastError || 'deploy failed', action: 'retry', tone: 'error',
      })
    } else if (phase === 'missing') {
      items.push({
        key: `machine:${machine.name}`, name: machine.name, view: 'machines', target: machine.name,
        reason: 'not reporting', action: 'inspect', tone: 'warning',
      })
    } else if (machine.powerState === 'stopped') {
      items.push({
        key: `machine:${machine.name}`, name: machine.name, view: 'machines', target: machine.name,
        reason: 'powered off', action: 'power on', tone: 'neutral',
      })
    }
  }

  for (const vm of virtualMachines) {
    const phase = vm.phase.toLowerCase()
    if (phase === 'error' || phase === 'missing') {
      items.push({
        key: `vm:${vm.name}`, name: vm.name, view: 'virtual-machines', target: vm.name,
        reason: vm.lastError || (phase === 'missing' ? 'not found on host' : 'error'),
        action: 'inspect', tone: phase === 'error' ? 'error' : 'warning',
      })
    }
  }

  const order = { error: 0, warning: 1, neutral: 2 }
  return items.sort((a, b) => order[a.tone] - order[b.tone])
}
