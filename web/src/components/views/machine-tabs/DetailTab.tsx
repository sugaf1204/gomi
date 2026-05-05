import { useEffect, useState } from 'react'
import { api } from '../../../api'
import type { HardwareInfo, Machine } from '../../../types'

type Props = {
  machine: Machine
}

export function DetailTab({ machine }: Props) {
  const [hw, setHw] = useState<HardwareInfo | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    api.getHardwareInfo(machine.name)
      .then(setHw)
      .catch(() => {
        setHw(null)
      })
      .finally(() => setLoading(false))
  }, [machine.name])

  if (loading) return <div className="pt-[0.65rem]"><p className="m-0 text-ink-soft">Loading hardware info...</p></div>
  if (!hw) return <div className="pt-[0.65rem]"><p className="m-0 text-ink-soft">No hardware details available yet.</p></div>

  return (
    <div className="pt-[0.65rem] grid gap-[0.85rem]">
      <Section title="CPU">
        <dl className="m-0 grid grid-cols-[120px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.25rem]">
          <dt className="text-ink-soft text-[0.84rem]">Model</dt><dd className="m-0">{hw.cpu.model}</dd>
          <dt className="text-ink-soft text-[0.84rem]">Cores / Threads</dt><dd className="m-0">{hw.cpu.cores} / {hw.cpu.threads}</dd>
          <dt className="text-ink-soft text-[0.84rem]">Architecture</dt><dd className="m-0">{hw.cpu.arch}</dd>
          {hw.cpu.mhz && <><dt className="text-ink-soft text-[0.84rem]">Clock</dt><dd className="m-0">{hw.cpu.mhz} MHz</dd></>}
        </dl>
      </Section>

      <Section title="Memory">
        <p className="m-0">{formatMB(hw.memory.totalMB)}</p>
      </Section>

      {hw.disks && hw.disks.length > 0 && (
        <Section title="Disks">
          <div className="grid gap-[0.3rem]">
            {hw.disks.map((d, i) => (
              <p key={i} className="m-0 text-[0.88rem]">
                <strong>{d.name}</strong> — {formatMB(d.sizeMB)} {d.type && `(${d.type})`} {d.model && `- ${d.model}`}
              </p>
            ))}
          </div>
        </Section>
      )}

      {hw.nics && hw.nics.length > 0 && (
        <Section title="Network Interfaces">
          <div className="grid gap-[0.3rem]">
            {hw.nics.map((n, i) => (
              <p key={i} className="m-0 text-[0.88rem]">
                <strong>{n.name}</strong> — {n.mac} {n.speed && `- ${n.speed}`} {n.state && `(${n.state})`}
              </p>
            ))}
          </div>
        </Section>
      )}

      {hw.bios && (hw.bios.vendor || hw.bios.version) && (
        <Section title="BIOS">
          <p className="m-0">{[hw.bios.vendor, hw.bios.version, hw.bios.date].filter(Boolean).join(' - ')}</p>
        </Section>
      )}

      {hw.gpus && hw.gpus.length > 0 && (
        <Section title="GPUs">
          <div className="grid gap-[0.3rem]">
            {hw.gpus.map((g, i) => (
              <p key={i} className="m-0 text-[0.88rem]">
                <strong>{g.name}</strong> {g.vendor && `- ${g.vendor}`} {g.vram && `- ${g.vram}`}
              </p>
            ))}
          </div>
        </Section>
      )}

      {hw.pci && hw.pci.length > 0 && (
        <Section title="PCI Devices">
          <div className="grid gap-[0.2rem] max-h-[200px] overflow-auto">
            {hw.pci.map((p, i) => (
              <p key={i} className="m-0 text-[0.82rem] font-mono">{p.slot} {p.class} {p.vendor} {p.device}</p>
            ))}
          </div>
        </Section>
      )}

      {hw.sensors && hw.sensors.length > 0 && (
        <Section title="Sensors">
          <div className="grid gap-[0.2rem]">
            {hw.sensors.map((s, i) => (
              <p key={i} className="m-0 text-[0.88rem]">{s.name}: {s.value} {s.unit}</p>
            ))}
          </div>
        </Section>
      )}
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h4 className="text-[0.92rem] font-medium mb-[0.35rem] text-ink-soft">{title}</h4>
      {children}
    </div>
  )
}

function formatMB(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb} MB`
}
