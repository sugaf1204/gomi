import type { AddressRange, Subnet } from '../types'

export type BandKind = 'reserved' | 'static' | 'free' | 'pxe'

export type AddressBand = {
  kind: BandKind
  /** Inclusive offset from the network address. */
  startOffset: number
  /** Inclusive offset from the network address. */
  endOffset: number
  label?: string
}

export type AddressSpace = {
  totalAddresses: number
  bands: AddressBand[]
  ticks: { offset: number; label: string }[]
}

/**
 * Precedence when regions overlap. A higher-precedence kind wins the address,
 * so an address claimed by both a reserved range and the PXE pool renders once,
 * as reserved. `free` is the fallback for addresses no region claims.
 */
const KIND_PRECEDENCE: BandKind[] = ['reserved', 'static', 'pxe']

/** Parses dotted-quad IPv4 into a 32-bit unsigned number, or null. */
export function parseIPv4(value: string): number | null {
  const parts = value.trim().split('.')
  if (parts.length !== 4) return null
  let result = 0
  for (const part of parts) {
    if (!/^\d{1,3}$/.test(part)) return null
    const octet = Number(part)
    if (octet > 255) return null
    result = result * 256 + octet
  }
  return result >>> 0
}

/** The host-octet label used on the ruler, e.g. `.11` for 192.168.2.11. */
function hostLabel(address: number): string {
  return `.${address & 0xff}`
}

type ParsedCIDR = { network: number; totalAddresses: number }

function parseCIDR(cidr: string | undefined): ParsedCIDR | null {
  if (!cidr) return null
  const [addressPart, prefixPart, ...rest] = cidr.trim().split('/')
  if (rest.length > 0 || prefixPart === undefined) return null
  if (!/^\d{1,2}$/.test(prefixPart)) return null
  const prefix = Number(prefixPart)
  if (prefix < 0 || prefix > 32) return null
  const address = parseIPv4(addressPart)
  if (address === null) return null
  // Mask the address down to the network boundary so a host-form CIDR such as
  // 192.168.2.10/24 still describes the 192.168.2.0 space.
  const hostBits = 32 - prefix
  const mask = hostBits === 32 ? 0 : (0xffffffff << hostBits) >>> 0
  const network = (address & mask) >>> 0
  const totalAddresses = hostBits === 32 ? 0x100000000 : 2 ** hostBits
  return { network, totalAddresses }
}

type Claim = { kind: BandKind; startOffset: number; endOffset: number }

/**
 * Converts an address range into an offset claim clamped to the subnet, or null
 * when the range is unparseable, inverted, or entirely outside the subnet.
 */
function claimFromRange(range: AddressRange | undefined, kind: BandKind, space: ParsedCIDR): Claim | null {
  if (!range) return null
  const start = parseIPv4(range.start)
  const end = parseIPv4(range.end)
  if (start === null || end === null) return null
  const low = Math.min(start, end)
  const high = Math.max(start, end)
  return clampClaim(low - space.network, high - space.network, kind, space)
}

function clampClaim(rawStart: number, rawEnd: number, kind: BandKind, space: ParsedCIDR): Claim | null {
  const startOffset = Math.max(rawStart, 0)
  const endOffset = Math.min(rawEnd, space.totalAddresses - 1)
  if (startOffset > endOffset) return null
  return { kind, startOffset, endOffset }
}

function staticClaims(staticIPs: string[], space: ParsedCIDR): Claim[] {
  const claims: Claim[] = []
  for (const ip of staticIPs) {
    const address = parseIPv4(ip)
    if (address === null) continue
    const offset = address - space.network
    // Addresses outside the subnet belong to another space; ignore them.
    if (offset < 0 || offset >= space.totalAddresses) continue
    claims.push({ kind: 'static', startOffset: offset, endOffset: offset })
  }
  return claims
}

/**
 * Paints claims onto a per-address kind map, highest precedence last so it wins
 * the overlap, then reads the map back as contiguous runs. This makes overlap
 * resolution total: every address ends up in exactly one band.
 */
function bandsFromClaims(claims: Claim[], totalAddresses: number): AddressBand[] {
  const owners: BandKind[] = new Array<BandKind>(totalAddresses).fill('free')
  // Lowest precedence first so later writes overwrite earlier ones.
  const ordered = [...KIND_PRECEDENCE].reverse()
  for (const kind of ordered) {
    for (const claim of claims) {
      if (claim.kind !== kind) continue
      for (let offset = claim.startOffset; offset <= claim.endOffset; offset += 1) {
        owners[offset] = kind
      }
    }
  }

  const bands: AddressBand[] = []
  let runStart = 0
  for (let offset = 1; offset <= totalAddresses; offset += 1) {
    if (offset === totalAddresses || owners[offset] !== owners[runStart]) {
      bands.push({ kind: owners[runStart], startOffset: runStart, endOffset: offset - 1 })
      runStart = offset
    }
  }
  return bands
}

/** One tick per band boundary, labelled with the host octet at that address. */
function ticksFromBands(bands: AddressBand[], network: number): { offset: number; label: string }[] {
  return bands.map((band) => ({
    offset: band.startOffset,
    label: hostLabel(network + band.startOffset),
  }))
}

/**
 * Lays out a subnet's address space as contiguous, non-overlapping bands.
 *
 * Reserved ranges, the PXE pool and machine static IPs are painted onto the
 * space; whatever they do not claim is free. Returns null when the subnet has
 * no parseable IPv4 CIDR, so callers can omit the visualisation rather than
 * render a misleading one. Spaces wider than a /16 are also rejected: the ruler
 * cannot show individual addresses at that scale.
 */
export function computeAddressSpace(subnet: Subnet, staticIPs: string[]): AddressSpace | null {
  const space = parseCIDR(subnet.spec?.cidr)
  if (!space) return null
  if (space.totalAddresses > 65536) return null

  const claims: Claim[] = []
  for (const range of subnet.spec.reservedRanges ?? []) {
    const claim = claimFromRange(range, 'reserved', space)
    if (claim) claims.push(claim)
  }
  const pxeClaim = claimFromRange(subnet.spec.pxeAddressRange, 'pxe', space)
  if (pxeClaim) claims.push(pxeClaim)
  claims.push(...staticClaims(staticIPs, space))

  const bands = bandsFromClaims(claims, space.totalAddresses)
  return {
    totalAddresses: space.totalAddresses,
    bands,
    ticks: ticksFromBands(bands, space.network),
  }
}

const BAND_LABELS: Record<BandKind, string> = {
  reserved: 'reserved',
  static: 'static',
  free: 'free',
  pxe: 'pxe pool',
}

/** Human-readable name for a band kind, shared by the ruler and its legend. */
export function bandLabel(kind: BandKind): string {
  return BAND_LABELS[kind]
}

/** Total addresses a band covers. */
export function bandSize(band: AddressBand): number {
  return band.endOffset - band.startOffset + 1
}
