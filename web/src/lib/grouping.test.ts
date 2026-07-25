import { describe, expect, it } from 'vitest'
import { groupCountLabel, groupMachines } from './grouping'
import type { Hypervisor, Machine, Subnet } from '../types'

function makeMachine(overrides: Partial<Machine> & { name: string }): Machine {
  return {
    hostname: overrides.name,
    mac: '00:00:00:00:00:00',
    arch: 'x86_64',
    firmware: 'uefi',
    power: { type: 'manual' },
    osPreset: { family: 'ubuntu', version: '22.04', imageRef: 'ubuntu-22.04' },
    phase: 'ready',
    ...overrides,
  }
}

function makeSubnet(name: string, cidr: string): Subnet {
  return { name, spec: { cidr } }
}

function makeHypervisor(overrides: Partial<Hypervisor> & { name: string }): Hypervisor {
  return {
    connection: { type: 'tcp', host: '10.0.0.5' },
    phase: 'Ready',
    vmCount: 0,
    ...overrides,
  }
}

describe('groupMachines', () => {
  it('returns [] for empty input', () => {
    expect(groupMachines([], 'phase', { subnets: [], hypervisors: [] })).toEqual([])
  })

  describe('subnet mode', () => {
    const subnets = [makeSubnet('default', '10.0.0.0/24'), makeSubnet('lab', '10.1.0.0/24')]

    it('groups machines by subnetRef and surfaces the subnet CIDR as facts', () => {
      const machines = [
        makeMachine({ name: 'm1', subnetRef: 'default' }),
        makeMachine({ name: 'm2', subnetRef: 'lab' }),
        makeMachine({ name: 'm3', subnetRef: 'default' }),
      ]
      const groups = groupMachines(machines, 'subnet', { subnets, hypervisors: [] })
      expect(groups.map((g) => g.title)).toEqual(['default', 'lab'])
      const defaultGroup = groups.find((g) => g.title === 'default')!
      expect(defaultGroup.facts).toBe('10.0.0.0/24')
      // Stable order within the group: m1 then m3, not re-sorted.
      expect(defaultGroup.machines.map((m) => m.name)).toEqual(['m1', 'm3'])
    })

    it('buckets machines with no subnetRef into a trailing unassigned group', () => {
      const machines = [makeMachine({ name: 'm1' }), makeMachine({ name: 'm2', subnetRef: 'default' })]
      const groups = groupMachines(machines, 'subnet', { subnets, hypervisors: [] })
      expect(groups.map((g) => g.title)).toEqual(['default', 'unassigned'])
      expect(groups.at(-1)!.facts).toBe('')
      expect(groups.at(-1)!.machines.map((m) => m.name)).toEqual(['m1'])
    })

    it('buckets a machine whose subnetRef names an unknown subnet into unassigned', () => {
      const machines = [makeMachine({ name: 'm1', subnetRef: 'ghost' })]
      const groups = groupMachines(machines, 'subnet', { subnets, hypervisors: [] })
      expect(groups).toHaveLength(1)
      expect(groups[0]).toMatchObject({ title: 'unassigned', facts: '', key: 'unassigned' })
    })

    it('sorts unassigned last even when its title would otherwise sort first alphabetically', () => {
      const withZ = [makeSubnet('zeta', '10.9.0.0/24')]
      const machines = [makeMachine({ name: 'm1' }), makeMachine({ name: 'm2', subnetRef: 'zeta' })]
      const groups = groupMachines(machines, 'subnet', { subnets: withZ, hypervisors: [] })
      expect(groups.map((g) => g.title)).toEqual(['zeta', 'unassigned'])
    })
  })

  describe('hypervisor mode', () => {
    it('groups machines whose name matches a hypervisor machineRef', () => {
      const hypervisors = [
        makeHypervisor({ name: 'hv-01', machineRef: 'm1', capacity: { cpuCores: 32, memoryMB: 131072 } }),
      ]
      const machines = [makeMachine({ name: 'm1' }), makeMachine({ name: 'm2' })]
      const groups = groupMachines(machines, 'hypervisor', { subnets: [], hypervisors })
      expect(groups.map((g) => g.title)).toEqual(['hv-01', 'unassigned'])
      expect(groups[0].facts).toBe('32c · 128g · 10.0.0.5')
      expect(groups[0].machines.map((m) => m.name)).toEqual(['m1'])
      expect(groups[1].machines.map((m) => m.name)).toEqual(['m2'])
    })

    it('rounds memory to whole GB', () => {
      const hypervisors = [makeHypervisor({ name: 'hv-01', machineRef: 'm1', capacity: { cpuCores: 8, memoryMB: 100000 } })]
      const machines = [makeMachine({ name: 'm1' })]
      const groups = groupMachines(machines, 'hypervisor', { subnets: [], hypervisors })
      // 100000 / 1024 = 97.65625 -> rounds to 98g
      expect(groups[0].facts).toBe('8c · 98g · 10.0.0.5')
    })

    it('omits the capacity segment when capacity is undefined', () => {
      const hypervisors = [makeHypervisor({ name: 'hv-01', machineRef: 'm1', capacity: undefined })]
      const machines = [makeMachine({ name: 'm1' })]
      const groups = groupMachines(machines, 'hypervisor', { subnets: [], hypervisors })
      expect(groups[0].facts).toBe('10.0.0.5')
    })

    it('omits the host segment when the connection host is absent, leaving just capacity', () => {
      const hypervisors = [
        makeHypervisor({
          name: 'hv-01',
          machineRef: 'm1',
          capacity: { cpuCores: 4, memoryMB: 8192 },
          connection: { type: 'tcp', host: '' },
        }),
      ]
      const machines = [makeMachine({ name: 'm1' })]
      const groups = groupMachines(machines, 'hypervisor', { subnets: [], hypervisors })
      expect(groups[0].facts).toBe('4c · 8g')
    })
  })

  describe('phase mode', () => {
    it('groups by lowercased phase with empty facts', () => {
      const machines = [
        makeMachine({ name: 'm1', phase: 'Ready' }),
        makeMachine({ name: 'm2', phase: 'ready' }),
        makeMachine({ name: 'm3', phase: 'Error' }),
      ]
      const groups = groupMachines(machines, 'phase', { subnets: [], hypervisors: [] })
      expect(groups.map((g) => g.title)).toEqual(['error', 'ready'])
      expect(groups.find((g) => g.title === 'ready')!.machines.map((m) => m.name)).toEqual(['m1', 'm2'])
      expect(groups.every((g) => g.facts === '')).toBe(true)
    })
  })

  it('sorts group titles case-insensitively', () => {
    const subnets = [makeSubnet('Bravo', '10.2.0.0/24'), makeSubnet('alpha', '10.3.0.0/24')]
    const machines = [
      makeMachine({ name: 'm1', subnetRef: 'Bravo' }),
      makeMachine({ name: 'm2', subnetRef: 'alpha' }),
    ]
    const groups = groupMachines(machines, 'subnet', { subnets, hypervisors: [] })
    expect(groups.map((g) => g.title)).toEqual(['alpha', 'Bravo'])
  })

  it('never produces an empty group', () => {
    const machines = [makeMachine({ name: 'm1', subnetRef: 'default' })]
    const groups = groupMachines(machines, 'subnet', {
      subnets: [makeSubnet('default', '10.0.0.0/24'), makeSubnet('unused', '10.5.0.0/24')],
      hypervisors: [],
    })
    expect(groups).toHaveLength(1)
    expect(groups.every((g) => g.machines.length > 0)).toBe(true)
  })
})

describe('groupCountLabel', () => {
  it('uses singular for exactly one', () => {
    expect(groupCountLabel(1)).toBe('1 machine')
  })

  it('uses plural for zero and for more than one', () => {
    expect(groupCountLabel(0)).toBe('0 machines')
    expect(groupCountLabel(4)).toBe('4 machines')
  })
})
