import { describe, expect, it } from 'vitest'
import { deriveDeployTimeline, type DeployTimeline } from './deploy-timeline'
import type { ProvisionProgress, ProvisionTiming } from '../types'

const T0 = Date.parse('2026-01-01T10:00:00Z')

function iso(offsetMs: number): string {
  return new Date(T0 + offsetMs).toISOString()
}

// The core guarantee of the redesign: segments are contiguous, non-overlapping,
// and their durations sum exactly to the attempt total, so no interval is ever
// silently unaccounted for.
function expectReconciled(timeline: DeployTimeline) {
  expect(timeline.startMs).toBeDefined()
  expect(timeline.endMs).toBeDefined()
  expect(timeline.segments.length).toBeGreaterThan(0)
  expect(timeline.segments[0].startMs).toBe(timeline.startMs)
  expect(timeline.segments[timeline.segments.length - 1].endMs).toBe(timeline.endMs)
  let sum = 0
  for (let i = 0; i < timeline.segments.length; i += 1) {
    const segment = timeline.segments[i]
    expect(segment.endMs).toBeGreaterThan(segment.startMs)
    if (i > 0) expect(segment.startMs).toBe(timeline.segments[i - 1].endMs)
    sum += segment.endMs - segment.startMs
  }
  expect(sum).toBe(timeline.totalMs)
}

function happyPathTimings(): ProvisionTiming[] {
  return [
    { source: 'server', name: 'server.pxe.boot_script', eventType: 'marker', timestamp: iso(40_000) },
    { source: 'initramfs', name: 'initramfs.init_top', eventType: 'marker', monotonicSeconds: 4.28 },
    { source: 'runner', name: 'runner.dhcp', eventType: 'timing', result: 'success', startedAt: iso(70_000), finishedAt: iso(73_000), durationMs: 3000 },
    { source: 'runner', name: 'runner.inventory', eventType: 'timing', result: 'success', startedAt: iso(80_000), finishedAt: iso(82_000), durationMs: 2000 },
    { source: 'server', name: 'server.inventory.total', eventType: 'timing', result: 'success', startedAt: iso(82_000), finishedAt: iso(83_000), durationMs: 1000 },
    { source: 'server', name: 'server.curtin_config', eventType: 'timing', result: 'success', startedAt: iso(90_000), finishedAt: iso(90_400), durationMs: 400 },
    { source: 'server', name: 'server.artifact_transfer', eventType: 'timing', result: 'success', startedAt: iso(100_000), finishedAt: iso(146_000), durationMs: 46_000 },
    { source: 'curtin', name: 'cmd-install/stage-extract', eventType: 'finish', result: 'success', startedAt: iso(150_000), finishedAt: iso(200_000), durationMs: 50_000 },
    { source: 'unknown', name: 'image_applied', eventType: 'image_applied', timestamp: iso(210_000) },
    { source: 'server', name: 'server.reboot_to_os', eventType: 'timing', result: 'success', startedAt: iso(210_000), finishedAt: iso(300_000), durationMs: 90_000 },
    { source: 'server', name: 'server.install_complete', eventType: 'marker', timestamp: iso(300_000) },
  ]
}

describe('deriveDeployTimeline', () => {
  it('attributes every interval of a completed attempt with full markers', () => {
    const provision: ProvisionProgress = {
      startedAt: iso(0),
      completedAt: iso(300_000),
      lastSignalAt: iso(300_000),
      timings: happyPathTimings(),
    }
    const timeline = deriveDeployTimeline(provision, T0 + 400_000)

    expect(timeline.status).toBe('complete')
    expect(timeline.totalMs).toBe(300_000)
    expectReconciled(timeline)
    expect(timeline.segments.map((s) => s.phaseId)).toEqual([
      'power-on',
      'installer-boot',
      'inventory-config',
      'image-apply',
      'reboot-os',
    ])
    expect(timeline.segments.every((s) => s.kind === 'tracked')).toBe(true)
    expect(timeline.notices).toEqual([])
  })

  it('labels unattributable intervals of legacy data as untracked instead of hiding them', () => {
    // No server markers: first event is the inventory, and completion has no
    // reboot evidence — both ends must surface as untracked, not blank.
    const provision: ProvisionProgress = {
      startedAt: iso(0),
      completedAt: iso(300_000),
      timings: [
        { source: 'runner', name: 'runner.inventory', eventType: 'timing', result: 'success', startedAt: iso(80_000), finishedAt: iso(82_000), durationMs: 2000 },
        { source: 'server', name: 'server.artifact_transfer', eventType: 'timing', result: 'success', startedAt: iso(100_000), finishedAt: iso(146_000), durationMs: 46_000 },
      ],
    }
    const timeline = deriveDeployTimeline(provision, T0 + 400_000)

    expect(timeline.status).toBe('complete')
    expectReconciled(timeline)
    expect(timeline.segments.map((s) => s.phaseId)).toEqual([
      'untracked',
      'inventory-config',
      'image-apply',
      'untracked',
    ])
    const head = timeline.segments[0]
    expect(head.fromLabel).toBe('provisioning started')
    expect(head.toLabel).toBe('runner.inventory')
    const tail = timeline.segments[timeline.segments.length - 1]
    expect(tail.startMs).toBe(T0 + 146_000)
    expect(tail.toLabel).toBe('provisioning completed')
  })

  it('extends the last segment live for an in-progress attempt and reports stalls', () => {
    const provision: ProvisionProgress = {
      active: true,
      startedAt: iso(0),
      lastSignalAt: iso(146_000),
      timings: happyPathTimings().slice(0, 7),
    }
    const nowMs = T0 + 500_000
    const timeline = deriveDeployTimeline(provision, nowMs)

    expect(timeline.status).toBe('in-progress')
    expect(timeline.endMs).toBe(nowMs)
    expectReconciled(timeline)
    const last = timeline.segments[timeline.segments.length - 1]
    expect(last.kind).toBe('live')
    expect(last.phaseId).toBe('image-apply')
    expect(timeline.notices.some((n) => n.kind === 'stalled')).toBe(true)
  })

  it('shows a single live power-on segment when nothing has been heard yet', () => {
    const provision: ProvisionProgress = { active: true, startedAt: iso(0) }
    const timeline = deriveDeployTimeline(provision, T0 + 30_000)

    expect(timeline.status).toBe('in-progress')
    expectReconciled(timeline)
    expect(timeline.segments).toHaveLength(1)
    expect(timeline.segments[0].phaseId).toBe('power-on')
    expect(timeline.segments[0].kind).toBe('live')
  })

  it('marks a failed attempt and its final segment as failed', () => {
    const provision: ProvisionProgress = {
      startedAt: iso(0),
      finishedAt: iso(180_000),
      failureReason: 'curtin install failed',
      timings: [
        ...happyPathTimings().slice(0, 7),
        { source: 'curtin', name: 'failed', eventType: 'failed', result: 'failure', timestamp: iso(180_000), message: 'exit status 1' },
      ],
    }
    const timeline = deriveDeployTimeline(provision, T0 + 400_000)

    expect(timeline.status).toBe('failed')
    expect(timeline.endMs).toBe(T0 + 180_000)
    expectReconciled(timeline)
    const last = timeline.segments[timeline.segments.length - 1]
    expect(last.failed).toBe(true)
    expect(timeline.events.some((e) => e.failed)).toBe(true)
  })

  it('keeps monotonic-only events off the wall-clock timeline but in the event list', () => {
    const provision: ProvisionProgress = {
      startedAt: iso(0),
      completedAt: iso(300_000),
      timings: happyPathTimings(),
    }
    const timeline = deriveDeployTimeline(provision, T0 + 400_000)

    const initramfs = timeline.events.find((e) => e.name === 'initramfs.init_top')
    expect(initramfs).toBeDefined()
    expect(initramfs!.timeSource).toBe('monotonic')
    expect(initramfs!.startMs).toBeUndefined()
    expect(initramfs!.monotonicSeconds).toBeCloseTo(4.28)
    // It must not distort phase boundaries: installer-boot opens at the
    // boot-script marker, not at a fake provStart+4.28s position.
    const installerBoot = timeline.segments.find((s) => s.phaseId === 'installer-boot')
    expect(installerBoot!.startMs).toBe(T0 + 40_000)
  })

  it('falls back to the earliest event and reports it when the start time is missing', () => {
    const provision: ProvisionProgress = {
      completedAt: iso(300_000),
      timings: happyPathTimings(),
    }
    const timeline = deriveDeployTimeline(provision, T0 + 400_000)

    expect(timeline.startMs).toBe(T0 + 40_000)
    expectReconciled(timeline)
    expect(timeline.notices.some((n) => n.kind === 'missing-start')).toBe(true)
  })

  it('lists events with no usable timestamp instead of dropping them', () => {
    const provision: ProvisionProgress = {
      active: true,
      startedAt: iso(0),
      timings: [
        { source: 'runner', name: 'runner.dhcp', eventType: 'timing', startedAt: iso(70_000), finishedAt: iso(73_000), durationMs: 3000 },
        { source: 'runner', name: 'runner.mystery' },
      ],
    }
    const timeline = deriveDeployTimeline(provision, T0 + 100_000)

    expectReconciled(timeline)
    const mystery = timeline.events.find((e) => e.name === 'runner.mystery')
    expect(mystery!.timeSource).toBe('unresolved')
    expect(timeline.notices.some((n) => n.kind === 'unresolved-events')).toBe(true)
    // Ordering keeps it next to its predecessor.
    expect(timeline.events.map((e) => e.name)).toEqual(['runner.dhcp', 'runner.mystery'])
  })

  it('clamps events outside the attempt window and reports clock skew', () => {
    const provision: ProvisionProgress = {
      startedAt: iso(0),
      completedAt: iso(100_000),
      timings: [
        { source: 'server', name: 'server.pxe.boot_script', eventType: 'marker', timestamp: iso(40_000) },
        { source: 'runner', name: 'runner.inventory', eventType: 'timing', startedAt: iso(50_000), finishedAt: iso(250_000), durationMs: 200_000 },
      ],
    }
    const timeline = deriveDeployTimeline(provision, T0 + 400_000)

    expectReconciled(timeline)
    const inventory = timeline.events.find((e) => e.name === 'runner.inventory')
    expect(inventory!.endMs).toBe(T0 + 100_000)
    expect(timeline.notices.some((n) => n.kind === 'clock-skew')).toBe(true)
  })

  it('treats an inactive attempt without a terminal record as failed with a notice', () => {
    const provision: ProvisionProgress = {
      startedAt: iso(0),
      lastSignalAt: iso(90_000),
      timings: happyPathTimings().slice(0, 5),
    }
    const timeline = deriveDeployTimeline(provision, T0 + 400_000)

    expect(timeline.status).toBe('failed')
    expectReconciled(timeline)
    expect(timeline.notices.some((n) => n.kind === 'not-finalized')).toBe(true)
  })

  it('returns empty for a missing or blank provision', () => {
    expect(deriveDeployTimeline(undefined, T0).status).toBe('empty')
    expect(deriveDeployTimeline({}, T0).status).toBe('empty')
  })
})
