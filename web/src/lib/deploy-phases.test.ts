import { describe, expect, it } from 'vitest'
import { boundaryPhase, buildPhaseSegments, segmentAt } from './deploy-phases'
import type { ResolvedEvent } from './deploy-timeline'

const T0 = Date.parse('2026-01-01T10:00:00Z')

function event(partial: Partial<ResolvedEvent> & { name: string }): ResolvedEvent {
  return {
    index: 0,
    source: 'server',
    failed: false,
    timeSource: 'wall',
    ...partial,
  }
}

describe('boundaryPhase', () => {
  it('maps boundary events to their canonical phases', () => {
    expect(boundaryPhase(event({ name: 'server.pxe.boot_script' }))).toBe('installer-boot')
    expect(boundaryPhase(event({ name: 'initramfs.init_top', source: 'initramfs' }))).toBe('installer-boot')
    expect(boundaryPhase(event({ name: 'runner.inventory', source: 'runner' }))).toBe('inventory-config')
    expect(boundaryPhase(event({ name: 'server.inventory.store' }))).toBe('inventory-config')
    expect(boundaryPhase(event({ name: 'server.curtin_config' }))).toBe('inventory-config')
    expect(boundaryPhase(event({ name: 'server.artifact_transfer' }))).toBe('image-apply')
    expect(boundaryPhase(event({ name: 'cmd-install/stage-extract', source: 'curtin' }))).toBe('image-apply')
    expect(boundaryPhase(event({ name: 'image_applied', source: 'unknown' }))).toBe('reboot-os')
    expect(boundaryPhase(event({ name: 'server.reboot_to_os' }))).toBe('reboot-os')
    expect(boundaryPhase(event({ name: 'server.pxe.boot_script_local' }))).toBe('reboot-os')
    expect(boundaryPhase(event({ name: 'runner.dhcp', source: 'runner' }))).toBeUndefined()
  })
})

describe('buildPhaseSegments', () => {
  it('clamps out-of-order boundaries so canonical phase order always wins', () => {
    // A skewed reporter claims image-apply started before the inventory did.
    // The later phase is clamped up to the earlier boundary, which collapses
    // the inventory phase to zero length instead of inventing an ordering.
    const events = [
      event({ name: 'runner.inventory', startMs: T0 + 60_000 }),
      event({ name: 'server.artifact_transfer', startMs: T0 + 50_000 }),
    ]
    const segments = buildPhaseSegments(events, T0, T0 + 100_000, 'complete')
    const image = segments.find((s) => s.phaseId === 'image-apply')
    expect(segments.find((s) => s.phaseId === 'inventory-config')).toBeUndefined()
    expect(image).toBeDefined()
    expect(image!.startMs).toBe(T0 + 60_000)
    expect(segments[0].startMs).toBe(T0)
    expect(segments[segments.length - 1].endMs).toBe(T0 + 100_000)
    for (let i = 1; i < segments.length; i += 1) {
      expect(segments[i].startMs).toBe(segments[i - 1].endMs)
    }
  })

  it('returns nothing for an empty window', () => {
    expect(buildPhaseSegments([], T0, T0, 'complete')).toEqual([])
  })
})

describe('segmentAt', () => {
  it('locates the segment containing a given time', () => {
    const events = [
      event({ name: 'server.pxe.boot_script', startMs: T0 + 40_000 }),
      event({ name: 'runner.inventory', startMs: T0 + 80_000 }),
    ]
    const segments = buildPhaseSegments(events, T0, T0 + 120_000, 'complete')
    expect(segmentAt(segments, T0 + 10_000)?.phaseId).toBe('power-on')
    expect(segmentAt(segments, T0 + 50_000)?.phaseId).toBe('installer-boot')
    expect(segmentAt(segments, T0 + 200_000)?.phaseId).toBe(segments[segments.length - 1].phaseId)
    expect(segmentAt(segments, undefined)).toBeUndefined()
  })
})
