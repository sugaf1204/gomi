import { describe, expect, it } from 'vitest'
import { buildPaletteEntries, filterPalette, fuzzyScore, type PaletteSources } from './palette'
import { allNavItems } from './navigation'

describe('fuzzyScore', () => {
  it('returns 0 for an empty query', () => {
    expect(fuzzyScore('', 'Machines')).toBe(0)
  })

  it('scores a prefix match higher than a mid-string match', () => {
    const prefix = fuzzyScore('mach', 'Machines')
    const midString = fuzzyScore('mach', 'gpu-machine-01')
    expect(prefix).not.toBeNull()
    expect(midString).not.toBeNull()
    expect(prefix as number).toBeGreaterThan(midString as number)
  })

  it('matches a subsequence like "gw1" against "gpu-worker-01"', () => {
    expect(fuzzyScore('gw1', 'gpu-worker-01')).not.toBeNull()
  })

  it('returns null for a non-subsequence', () => {
    expect(fuzzyScore('zzz', 'gpu-worker-01')).toBeNull()
    expect(fuzzyScore('wg', 'gpu-worker-01')).toBeNull()
  })

  it('is case-insensitive', () => {
    expect(fuzzyScore('MACH', 'Machines')).toBe(fuzzyScore('mach', 'Machines'))
    expect(fuzzyScore('GpU', 'gpu-worker-01')).toEqual(fuzzyScore('gpu', 'gpu-worker-01'))
  })
})

describe('filterPalette', () => {
  const sources: PaletteSources = {
    machines: ['gpu-machine-01'],
    virtualMachines: ['vm-alpha'],
    subnets: ['lan-1'],
  }
  const entries = buildPaletteEntries(sources)

  it('returns all entries for an empty query', () => {
    expect(filterPalette(entries, '')).toHaveLength(entries.length)
  })

  it('orders results best-first, e.g. "mach" ranks nav "Machines" above "gpu-machine-01"', () => {
    const results = filterPalette(entries, 'mach')
    const machinesIndex = results.findIndex((entry) => entry.label === 'Machines')
    const gpuMachineIndex = results.findIndex((entry) => entry.label === 'gpu-machine-01')
    expect(machinesIndex).toBeGreaterThanOrEqual(0)
    expect(gpuMachineIndex).toBeGreaterThanOrEqual(0)
    expect(machinesIndex).toBeLessThan(gpuMachineIndex)
  })

  it('returns [] when the query matches nothing', () => {
    expect(filterPalette(entries, 'zzzzzzzzzz')).toEqual([])
  })
})

describe('buildPaletteEntries', () => {
  const sources: PaletteSources = {
    machines: ['gpu-machine-01', 'gpu-machine-02'],
    virtualMachines: ['vm-alpha'],
    subnets: ['lan-1'],
  }
  const entries = buildPaletteEntries(sources)

  it('includes all nav destinations', () => {
    const navEntries = entries.filter((entry) => entry.kind === 'nav')
    expect(navEntries).toHaveLength(allNavItems().length)
    for (const item of allNavItems()) {
      expect(navEntries.some((entry) => entry.label === item.label && entry.view === item.view)).toBe(true)
    }
  })

  it('includes the supplied machines, VMs, and subnets', () => {
    expect(entries.some((entry) => entry.kind === 'machine' && entry.label === 'gpu-machine-01')).toBe(true)
    expect(entries.some((entry) => entry.kind === 'machine' && entry.label === 'gpu-machine-02')).toBe(true)
    expect(entries.some((entry) => entry.kind === 'virtual machine' && entry.label === 'vm-alpha')).toBe(true)
    expect(entries.some((entry) => entry.kind === 'subnet' && entry.label === 'lan-1')).toBe(true)
  })

  it('assigns the right view and target for each kind', () => {
    const machine = entries.find((entry) => entry.kind === 'machine' && entry.label === 'gpu-machine-01')
    expect(machine).toMatchObject({ view: 'machines', target: 'gpu-machine-01' })

    const vm = entries.find((entry) => entry.kind === 'virtual machine' && entry.label === 'vm-alpha')
    expect(vm).toMatchObject({ view: 'virtual-machines', target: 'vm-alpha' })

    const subnet = entries.find((entry) => entry.kind === 'subnet' && entry.label === 'lan-1')
    expect(subnet).toMatchObject({ view: 'network', target: 'lan-1' })

    const nav = entries.find((entry) => entry.kind === 'nav' && entry.label === 'Machines')
    expect(nav).toMatchObject({ view: 'machines' })
    expect(nav?.target).toBeUndefined()
  })

  it('has unique ids across all entries', () => {
    const ids = entries.map((entry) => entry.id)
    expect(new Set(ids).size).toBe(ids.length)
  })
})
