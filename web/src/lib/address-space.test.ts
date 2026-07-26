import { describe, expect, it } from 'vitest'
import { bandLabel, bandSize, computeAddressSpace, parseIPv4 } from './address-space'
import type { AddressBand } from './address-space'
import type { Subnet } from '../types'

function makeSubnet(spec: Partial<Subnet['spec']> & { cidr: string }): Subnet {
  return { name: 'lab', spec }
}

/** Bands as compact tuples, for readable expectations. */
function shape(bands: AddressBand[]): [string, number, number][] {
  return bands.map((band) => [band.kind, band.startOffset, band.endOffset])
}

/** Every address belongs to exactly one band, in order, with no gaps. */
function expectContiguous(bands: AddressBand[], totalAddresses: number) {
  expect(bands.length).toBeGreaterThan(0)
  expect(bands[0].startOffset).toBe(0)
  expect(bands.at(-1)?.endOffset).toBe(totalAddresses - 1)
  for (const band of bands) {
    expect(band.endOffset).toBeGreaterThanOrEqual(band.startOffset)
  }
  for (let i = 1; i < bands.length; i += 1) {
    expect(bands[i].startOffset).toBe(bands[i - 1].endOffset + 1)
    expect(bands[i].kind).not.toBe(bands[i - 1].kind)
  }
  expect(bands.reduce((sum, band) => sum + bandSize(band), 0)).toBe(totalAddresses)
}

describe('parseIPv4', () => {
  it('parses dotted-quad addresses', () => {
    expect(parseIPv4('0.0.0.0')).toBe(0)
    expect(parseIPv4('192.168.2.1')).toBe(3232236033)
    expect(parseIPv4('255.255.255.255')).toBe(4294967295)
  })

  it('rejects malformed addresses', () => {
    expect(parseIPv4('192.168.2')).toBeNull()
    expect(parseIPv4('192.168.2.256')).toBeNull()
    expect(parseIPv4('192.168.2.a')).toBeNull()
    expect(parseIPv4('')).toBeNull()
  })
})

describe('computeAddressSpace', () => {
  it('lays out a /24 with reserved and pxe ranges', () => {
    const subnet = makeSubnet({
      cidr: '192.168.2.0/24',
      reservedRanges: [{ start: '192.168.2.1', end: '192.168.2.10' }],
      pxeAddressRange: { start: '192.168.2.200', end: '192.168.2.249' },
    })

    const space = computeAddressSpace(subnet, [])
    expect(space).not.toBeNull()
    expect(space?.totalAddresses).toBe(256)
    expect(shape(space!.bands)).toEqual([
      ['free', 0, 0],
      ['reserved', 1, 10],
      ['free', 11, 199],
      ['pxe', 200, 249],
      ['free', 250, 255],
    ])
    expectContiguous(space!.bands, 256)
  })

  it('labels ticks with the host octet at each band boundary', () => {
    const subnet = makeSubnet({
      cidr: '192.168.2.0/24',
      reservedRanges: [{ start: '192.168.2.1', end: '192.168.2.10' }],
      pxeAddressRange: { start: '192.168.2.200', end: '192.168.2.249' },
    })

    const space = computeAddressSpace(subnet, ['192.168.2.60'])
    expect(space?.ticks.map((tick) => tick.label)).toEqual(['.0', '.1', '.11', '.60', '.61', '.200', '.250'])
    expect(space?.ticks.map((tick) => tick.offset)).toEqual([0, 1, 11, 60, 61, 200, 250])
  })

  it('resolves overlapping ranges by precedence, keeping bands contiguous', () => {
    const subnet = makeSubnet({
      cidr: '192.168.2.0/24',
      // The reserved range overlaps the first half of the PXE pool.
      reservedRanges: [{ start: '192.168.2.100', end: '192.168.2.150' }],
      pxeAddressRange: { start: '192.168.2.120', end: '192.168.2.180' },
    })

    const space = computeAddressSpace(subnet, [])
    expect(shape(space!.bands)).toEqual([
      ['free', 0, 99],
      ['reserved', 100, 150],
      ['pxe', 151, 180],
      ['free', 181, 255],
    ])
    expectContiguous(space!.bands, 256)
  })

  it('merges two overlapping reserved ranges into one band', () => {
    const subnet = makeSubnet({
      cidr: '10.0.0.0/24',
      reservedRanges: [
        { start: '10.0.0.10', end: '10.0.0.40' },
        { start: '10.0.0.30', end: '10.0.0.60' },
      ],
    })

    const space = computeAddressSpace(subnet, [])
    expect(shape(space!.bands)).toEqual([
      ['free', 0, 9],
      ['reserved', 10, 60],
      ['free', 61, 255],
    ])
  })

  it('clamps a range that extends past the end of the subnet', () => {
    const subnet = makeSubnet({
      cidr: '192.168.2.0/24',
      pxeAddressRange: { start: '192.168.2.240', end: '192.168.3.99' },
    })

    const space = computeAddressSpace(subnet, [])
    expect(shape(space!.bands)).toEqual([
      ['free', 0, 239],
      ['pxe', 240, 255],
    ])
    expectContiguous(space!.bands, 256)
  })

  it('clamps a range that starts before the subnet', () => {
    const subnet = makeSubnet({
      cidr: '192.168.2.0/24',
      reservedRanges: [{ start: '192.168.1.200', end: '192.168.2.5' }],
    })

    const space = computeAddressSpace(subnet, [])
    expect(shape(space!.bands)).toEqual([
      ['reserved', 0, 5],
      ['free', 6, 255],
    ])
  })

  it('drops a range that lies entirely outside the subnet', () => {
    const subnet = makeSubnet({
      cidr: '192.168.2.0/24',
      reservedRanges: [{ start: '10.9.9.1', end: '10.9.9.20' }],
    })

    const space = computeAddressSpace(subnet, [])
    expect(shape(space!.bands)).toEqual([['free', 0, 255]])
  })

  it('renders one free band when there are no reserved or pxe ranges', () => {
    const space = computeAddressSpace(makeSubnet({ cidr: '10.1.2.0/24' }), [])
    expect(shape(space!.bands)).toEqual([['free', 0, 255]])
    expect(space?.ticks).toEqual([{ offset: 0, label: '.0' }])
    expectContiguous(space!.bands, 256)
  })

  it('places static IPs inside the subnet and ignores those outside', () => {
    const subnet = makeSubnet({ cidr: '192.168.2.0/24' })
    const space = computeAddressSpace(subnet, ['192.168.2.20', '10.0.0.5', '192.168.3.20', 'not-an-ip', ''])

    expect(shape(space!.bands)).toEqual([
      ['free', 0, 19],
      ['static', 20, 20],
      ['free', 21, 255],
    ])
    expectContiguous(space!.bands, 256)
  })

  it('merges adjacent static IPs into a single band', () => {
    const subnet = makeSubnet({ cidr: '192.168.2.0/24' })
    const space = computeAddressSpace(subnet, ['192.168.2.11', '192.168.2.12', '192.168.2.13'])

    expect(shape(space!.bands)).toEqual([
      ['free', 0, 10],
      ['static', 11, 13],
      ['free', 14, 255],
    ])
  })

  it('lets a reserved range win over a static IP inside it', () => {
    const subnet = makeSubnet({
      cidr: '192.168.2.0/24',
      reservedRanges: [{ start: '192.168.2.1', end: '192.168.2.10' }],
    })
    const space = computeAddressSpace(subnet, ['192.168.2.5'])

    expect(shape(space!.bands)).toEqual([
      ['free', 0, 0],
      ['reserved', 1, 10],
      ['free', 11, 255],
    ])
  })

  it('lets a static IP win over the pxe pool it sits in', () => {
    const subnet = makeSubnet({
      cidr: '192.168.2.0/24',
      pxeAddressRange: { start: '192.168.2.100', end: '192.168.2.110' },
    })
    const space = computeAddressSpace(subnet, ['192.168.2.105'])

    expect(shape(space!.bands)).toEqual([
      ['free', 0, 99],
      ['pxe', 100, 104],
      ['static', 105, 105],
      ['pxe', 106, 110],
      ['free', 111, 255],
    ])
    expectContiguous(space!.bands, 256)
  })

  it('handles a /30 edge case', () => {
    const subnet = makeSubnet({
      cidr: '10.0.0.0/30',
      reservedRanges: [{ start: '10.0.0.0', end: '10.0.0.1' }],
    })
    const space = computeAddressSpace(subnet, ['10.0.0.2'])

    expect(space?.totalAddresses).toBe(4)
    expect(shape(space!.bands)).toEqual([
      ['reserved', 0, 1],
      ['static', 2, 2],
      ['free', 3, 3],
    ])
    expect(space?.ticks).toEqual([
      { offset: 0, label: '.0' },
      { offset: 2, label: '.2' },
      { offset: 3, label: '.3' },
    ])
    expectContiguous(space!.bands, 4)
  })

  it('masks a host-form CIDR down to its network address', () => {
    const subnet = makeSubnet({
      cidr: '192.168.2.37/24',
      reservedRanges: [{ start: '192.168.2.1', end: '192.168.2.4' }],
    })
    const space = computeAddressSpace(subnet, [])

    expect(space?.totalAddresses).toBe(256)
    expect(shape(space!.bands)).toEqual([
      ['free', 0, 0],
      ['reserved', 1, 4],
      ['free', 5, 255],
    ])
  })

  it('returns null for a missing, malformed or IPv6 CIDR', () => {
    expect(computeAddressSpace(makeSubnet({ cidr: '' }), [])).toBeNull()
    expect(computeAddressSpace(makeSubnet({ cidr: '192.168.2.0' }), [])).toBeNull()
    expect(computeAddressSpace(makeSubnet({ cidr: '192.168.2.0/33' }), [])).toBeNull()
    expect(computeAddressSpace(makeSubnet({ cidr: '192.168.2.0/abc' }), [])).toBeNull()
    expect(computeAddressSpace(makeSubnet({ cidr: '999.1.1.0/24' }), [])).toBeNull()
    expect(computeAddressSpace(makeSubnet({ cidr: '2001:db8::/32' }), [])).toBeNull()
    expect(computeAddressSpace(makeSubnet({ cidr: '10.0.0.0/24/8' }), [])).toBeNull()
  })

  it('returns null for a space too wide to draw per-address', () => {
    expect(computeAddressSpace(makeSubnet({ cidr: '10.0.0.0/8' }), [])).toBeNull()
    expect(computeAddressSpace(makeSubnet({ cidr: '10.0.0.0/16' }), [])).not.toBeNull()
  })
})

describe('bandLabel', () => {
  it('names every band kind', () => {
    expect(bandLabel('reserved')).toBe('reserved')
    expect(bandLabel('static')).toBe('static')
    expect(bandLabel('free')).toBe('free')
    expect(bandLabel('pxe')).toBe('pxe pool')
  })
})
