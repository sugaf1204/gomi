import type { Hypervisor, Machine, Subnet } from '../types'

export type GroupBy = 'subnet' | 'hypervisor' | 'phase'

export type MachineGroup = {
  key: string
  title: string
  facts: string
  machines: Machine[]
}

const UNASSIGNED_KEY = 'unassigned'
const UNASSIGNED_TITLE = 'unassigned'

/**
 * Builds the grouping context lookups once per call so each machine lookup
 * is O(1) rather than re-scanning the subnet/hypervisor arrays per machine.
 */
function buildSubnetIndex(subnets: Subnet[]): Map<string, Subnet> {
  return new Map(subnets.map((subnet) => [subnet.name, subnet]))
}

// A machine belongs to a hypervisor when that hypervisor's machineRef points
// back at it, so the lookup is keyed by machineRef rather than by name.
function buildHypervisorByMachineIndex(hypervisors: Hypervisor[]): Map<string, Hypervisor> {
  const index = new Map<string, Hypervisor>()
  for (const hypervisor of hypervisors) {
    if (hypervisor.machineRef) index.set(hypervisor.machineRef, hypervisor)
  }
  return index
}

function subnetFacts(subnet: Subnet | undefined): string {
  return subnet?.spec.cidr ?? ''
}

/** Renders whole-GB memory, e.g. 131072MB -> "128g". */
function memoryGB(memoryMB: number): string {
  return `${Math.round(memoryMB / 1024)}g`
}

function hypervisorFacts(hypervisor: Hypervisor): string {
  const parts: string[] = []
  if (hypervisor.capacity) {
    parts.push(`${hypervisor.capacity.cpuCores}c · ${memoryGB(hypervisor.capacity.memoryMB)}`)
  }
  if (hypervisor.connection?.host) {
    parts.push(hypervisor.connection.host)
  }
  return parts.join(' · ')
}

type Bucket = { title: string; facts: string; machines: Machine[] }

/** Appends a machine to its bucket, creating the bucket on first use so no empty groups are ever produced. */
function pushToBucket(buckets: Map<string, Bucket>, key: string, title: string, facts: string, machine: Machine) {
  const existing = buckets.get(key)
  if (existing) {
    existing.machines.push(machine)
    return
  }
  buckets.set(key, { title, facts, machines: [machine] })
}

function groupBySubnet(machines: Machine[], subnets: Subnet[]): Map<string, Bucket> {
  const subnetIndex = buildSubnetIndex(subnets)
  const buckets = new Map<string, Bucket>()
  for (const machine of machines) {
    const subnet = machine.subnetRef ? subnetIndex.get(machine.subnetRef) : undefined
    if (!subnet) {
      pushToBucket(buckets, UNASSIGNED_KEY, UNASSIGNED_TITLE, '', machine)
      continue
    }
    pushToBucket(buckets, subnet.name, subnet.name, subnetFacts(subnet), machine)
  }
  return buckets
}

function groupByHypervisor(machines: Machine[], hypervisors: Hypervisor[]): Map<string, Bucket> {
  const hypervisorIndex = buildHypervisorByMachineIndex(hypervisors)
  const buckets = new Map<string, Bucket>()
  for (const machine of machines) {
    const hypervisor = hypervisorIndex.get(machine.name)
    if (!hypervisor) {
      pushToBucket(buckets, UNASSIGNED_KEY, UNASSIGNED_TITLE, '', machine)
      continue
    }
    pushToBucket(buckets, hypervisor.name, hypervisor.name, hypervisorFacts(hypervisor), machine)
  }
  return buckets
}

function groupByPhase(machines: Machine[]): Map<string, Bucket> {
  const buckets = new Map<string, Bucket>()
  for (const machine of machines) {
    const title = (machine.phase ?? '').toLowerCase()
    pushToBucket(buckets, title, title, '', machine)
  }
  return buckets
}

/**
 * Groups machines under header rows for the fleet list. The unassigned
 * bucket (machines with no matching subnet/hypervisor) always sorts last;
 * every other group sorts by title, case-insensitively. Machines keep their
 * incoming relative order within a group.
 */
export function groupMachines(
  machines: Machine[],
  groupBy: GroupBy,
  context: { subnets: Subnet[]; hypervisors: Hypervisor[] }
): MachineGroup[] {
  if (machines.length === 0) return []

  const buckets =
    groupBy === 'subnet'
      ? groupBySubnet(machines, context.subnets)
      : groupBy === 'hypervisor'
        ? groupByHypervisor(machines, context.hypervisors)
        : groupByPhase(machines)

  const entries = [...buckets.entries()]
  entries.sort(([keyA, a], [keyB, b]) => {
    if (keyA === UNASSIGNED_KEY) return keyB === UNASSIGNED_KEY ? 0 : 1
    if (keyB === UNASSIGNED_KEY) return -1
    return a.title.localeCompare(b.title, undefined, { sensitivity: 'base' })
  })

  return entries.map(([key, bucket]) => ({
    key,
    title: bucket.title,
    facts: bucket.facts,
    machines: bucket.machines,
  }))
}

/** "1 machine" / "4 machines" for group header row counts. */
export function groupCountLabel(count: number): string {
  return count === 1 ? '1 machine' : `${count} machines`
}
