import type { Hypervisor, Machine, Subnet, VirtualMachine } from '../types'

export type GroupBy = 'subnet' | 'hypervisor' | 'phase'

export type MachineGroup = {
  key: string
  title: string
  facts: string
  machines: Machine[]
}

export type VMGroup = {
  key: string
  title: string
  facts: string
  virtualMachines: VirtualMachine[]
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

type Bucket<T> = { title: string; facts: string; items: T[] }

/** Appends an item to its bucket, creating the bucket on first use so no empty groups are ever produced. */
function pushToBucket<T>(buckets: Map<string, Bucket<T>>, key: string, title: string, facts: string, item: T) {
  const existing = buckets.get(key)
  if (existing) {
    existing.items.push(item)
    return
  }
  buckets.set(key, { title, facts, items: [item] })
}

/**
 * Orders buckets for rendering: the unassigned bucket always sorts last, every
 * other bucket sorts by title case-insensitively. Items keep their incoming
 * relative order because bucketing only ever appends.
 */
function sortedBucketEntries<T>(buckets: Map<string, Bucket<T>>): Array<[string, Bucket<T>]> {
  const entries = [...buckets.entries()]
  entries.sort(([keyA, a], [keyB, b]) => {
    if (keyA === UNASSIGNED_KEY) return keyB === UNASSIGNED_KEY ? 0 : 1
    if (keyB === UNASSIGNED_KEY) return -1
    return a.title.localeCompare(b.title, undefined, { sensitivity: 'base' })
  })
  return entries
}

function groupBySubnet(machines: Machine[], subnets: Subnet[]): Map<string, Bucket<Machine>> {
  const subnetIndex = buildSubnetIndex(subnets)
  const buckets = new Map<string, Bucket<Machine>>()
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

function groupByHypervisor(machines: Machine[], hypervisors: Hypervisor[]): Map<string, Bucket<Machine>> {
  const hypervisorIndex = buildHypervisorByMachineIndex(hypervisors)
  const buckets = new Map<string, Bucket<Machine>>()
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

function groupByPhase(machines: Machine[]): Map<string, Bucket<Machine>> {
  const buckets = new Map<string, Bucket<Machine>>()
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

  return sortedBucketEntries(buckets).map(([key, bucket]) => ({
    key,
    title: bucket.title,
    facts: bucket.facts,
    machines: bucket.items,
  }))
}

/**
 * Header facts for a VM group, e.g. "32c · 128g · 10.0.0.5 · 4 vms". Segments
 * with no backing data are omitted rather than rendered as placeholders, so a
 * hypervisor without reported capacity still shows its host and VM count.
 */
function vmGroupFacts(hypervisor: Hypervisor, vmCount: number): string {
  const parts: string[] = []
  if (hypervisor.capacity) {
    parts.push(`${hypervisor.capacity.cpuCores}c`, memoryGB(hypervisor.capacity.memoryMB))
  }
  if (hypervisor.connection?.host) parts.push(hypervisor.connection.host)
  parts.push(vmCountLabel(vmCount))
  return parts.join(' · ')
}

/**
 * Groups virtual machines under their hypervisor for the VM list. VMs whose
 * hypervisorRef is empty or names a hypervisor we do not know about fall into a
 * trailing unassigned group. Sorting and stability match groupMachines.
 */
export function groupVirtualMachines(vms: VirtualMachine[], hypervisors: Hypervisor[]): VMGroup[] {
  if (vms.length === 0) return []

  const hypervisorIndex = new Map(hypervisors.map((hypervisor) => [hypervisor.name, hypervisor]))
  const buckets = new Map<string, Bucket<VirtualMachine>>()
  // Facts include the group's own VM count, so they are filled in after
  // bucketing rather than at first-insert time.
  const bucketHypervisors = new Map<string, Hypervisor>()

  for (const vm of vms) {
    const ref = vm.hypervisorRef || vm.hypervisorName || ''
    const hypervisor = ref ? hypervisorIndex.get(ref) : undefined
    if (!hypervisor) {
      pushToBucket(buckets, UNASSIGNED_KEY, UNASSIGNED_TITLE, '', vm)
      continue
    }
    bucketHypervisors.set(hypervisor.name, hypervisor)
    pushToBucket(buckets, hypervisor.name, hypervisor.name, '', vm)
  }

  return sortedBucketEntries(buckets).map(([key, bucket]) => {
    const hypervisor = bucketHypervisors.get(key)
    return {
      key,
      title: bucket.title,
      facts: hypervisor ? vmGroupFacts(hypervisor, bucket.items.length) : '',
      virtualMachines: bucket.items,
    }
  })
}

/** "1 machine" / "4 machines" for group header row counts. */
export function groupCountLabel(count: number): string {
  return count === 1 ? '1 machine' : `${count} machines`
}

/** "1 vm" / "4 vms" for VM group header row counts and facts. */
export function vmCountLabel(count: number): string {
  return count === 1 ? '1 vm' : `${count} vms`
}
