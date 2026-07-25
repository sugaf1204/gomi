import { describe, expect, it } from 'vitest'
import { buildBreadcrumb } from './breadcrumb'

describe('buildBreadcrumb', () => {
  it('renders a single current segment for overview', () => {
    expect(buildBreadcrumb('overview')).toEqual([{ label: 'overview', kind: 'current' }])
  })

  it('places fleet views under the fleet ancestor', () => {
    expect(buildBreadcrumb('machines')).toEqual([
      { label: 'fleet', kind: 'ancestor' },
      { label: 'machines', kind: 'current' },
    ])
  })

  it('groups catalog and network views under their own ancestors', () => {
    expect(buildBreadcrumb('os-images')[0]).toEqual({ label: 'catalog', kind: 'ancestor' })
    expect(buildBreadcrumb('dns-records')[0]).toEqual({ label: 'network', kind: 'ancestor' })
  })

  it('demotes every view segment to ancestor when an object is selected', () => {
    expect(buildBreadcrumb('network', 'default')).toEqual([
      { label: 'network', kind: 'ancestor' },
      { label: 'subnets', kind: 'ancestor' },
      { label: 'default', kind: 'object' },
    ])
  })

  it('ignores an empty selected object', () => {
    expect(buildBreadcrumb('machines', '')).toEqual(buildBreadcrumb('machines'))
  })

  it('covers every view in the hierarchy', () => {
    const views = [
      'overview', 'machines', 'virtual-machines', 'hypervisors', 'os-images',
      'cloud-init', 'network', 'dns-records', 'activity', 'users', 'settings',
    ] as const
    for (const view of views) {
      const crumbs = buildBreadcrumb(view)
      expect(crumbs.length).toBeGreaterThan(0)
      expect(crumbs.at(-1)?.kind).toBe('current')
    }
  })
})
