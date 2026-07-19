import type { DeployStatus, ResolvedEvent } from './deploy-timeline'

export type PhaseId =
  | 'power-on'
  | 'installer-boot'
  | 'inventory-config'
  | 'image-apply'
  | 'reboot-os'
  | 'untracked'

export type SegmentKind = 'tracked' | 'untracked' | 'live'

export type PhaseSegment = {
  phaseId: PhaseId
  label: string
  kind: SegmentKind
  startMs: number
  endMs: number
  failed: boolean
  fromLabel: string
  toLabel: string
}

export const PHASE_LABELS: Record<PhaseId, string> = {
  'power-on': 'Power-on & PXE boot',
  'installer-boot': 'Installer boot',
  'inventory-config': 'Inventory & config',
  'image-apply': 'Image transfer & apply',
  'reboot-os': 'Reboot into target OS',
  untracked: 'Untracked',
}

const BOUNDARY_PHASES: PhaseId[] = ['installer-boot', 'inventory-config', 'image-apply', 'reboot-os']

// A completed attempt whose tail is attributed purely by inference is split
// into an explicit untracked segment when the gap exceeds this threshold.
const TAIL_GAP_EPSILON_MS = 1500

// Maps an event to the deploy phase it opens. Name matches take priority over
// source matches because sources like "curtin" span an entire stage while
// names mark exact boundaries.
export function boundaryPhase(event: ResolvedEvent): PhaseId | undefined {
  const name = event.name
  if (
    name === 'image_applied' ||
    event.eventType === 'image_applied' ||
    name === 'server.reboot_to_os' ||
    name === 'server.pxe.boot_script_local'
  ) {
    return 'reboot-os'
  }
  if (
    name === 'runner.inventory' ||
    name.startsWith('server.inventory') ||
    name === 'server.curtin_config' ||
    name === 'runner.fetch_curtin_config'
  ) {
    return 'inventory-config'
  }
  if (name === 'server.artifact_transfer' || name === 'runner.write_disk_image') {
    return 'image-apply'
  }
  if (name === 'server.pxe.boot_script') {
    return 'installer-boot'
  }
  if (event.source === 'initramfs') return 'installer-boot'
  if (event.source === 'curtin') return 'image-apply'
  return undefined
}

function endLabel(status: DeployStatus): string {
  if (status === 'complete') return 'provisioning completed'
  if (status === 'failed') return 'provisioning failed'
  return 'last signal'
}

type Anchor = { timeMs: number; phaseId: PhaseId; opener: string }

function collectAnchors(events: ResolvedEvent[], startMs: number, endMs: number): Anchor[] {
  const earliest = new Map<PhaseId, Anchor>()
  for (const event of events) {
    const phaseId = boundaryPhase(event)
    if (!phaseId) continue
    const timeMs = event.startMs ?? event.endMs
    if (timeMs === undefined) continue
    const existing = earliest.get(phaseId)
    if (!existing || timeMs < existing.timeMs) {
      earliest.set(phaseId, { timeMs, phaseId, opener: event.name })
    }
  }
  // Canonical phase order wins over raw timestamps: a later phase's boundary
  // closes every earlier phase, so anchors are clamped monotonically.
  const anchors: Anchor[] = []
  let floor = startMs
  for (const phaseId of BOUNDARY_PHASES) {
    const anchor = earliest.get(phaseId)
    if (!anchor) continue
    const timeMs = Math.min(Math.max(anchor.timeMs, floor), endMs)
    anchors.push({ ...anchor, timeMs })
    floor = timeMs
  }
  return anchors
}

// Partitions [startMs, endMs] into contiguous, non-overlapping segments so
// that every millisecond of the attempt is attributed to a named phase or to
// an explicitly labeled untracked interval. Invariant: sum of segment
// durations equals endMs - startMs.
export function buildPhaseSegments(
  events: ResolvedEvent[],
  startMs: number,
  endMs: number,
  status: DeployStatus,
): PhaseSegment[] {
  if (!(endMs > startMs)) return []
  const segments: PhaseSegment[] = []
  const push = (
    phaseId: PhaseId,
    kind: SegmentKind,
    from: number,
    to: number,
    fromLabel: string,
    toLabel: string,
    failed = false,
  ) => {
    if (to - from <= 0) return
    segments.push({ phaseId, label: PHASE_LABELS[phaseId], kind, startMs: from, endMs: to, failed, fromLabel, toLabel })
  }

  const anchors = collectAnchors(events, startMs, endMs)
  if (anchors.length === 0) {
    if (status === 'in-progress') {
      // Nothing heard yet, but the attempt start is a fact: the machine is
      // powering on and netbooting until it makes first contact.
      push('power-on', 'live', startMs, endMs, 'provisioning started', 'now')
    } else {
      push('untracked', 'untracked', startMs, endMs, 'provisioning started', endLabel(status))
    }
    return segments
  }

  const first = anchors[0]
  if (first.timeMs > startMs) {
    // The interval before first contact is only attributable when the first
    // boundary is the installer-boot handoff; otherwise earlier phases are
    // folded into it indistinguishably and claiming one would be a guess.
    if (first.phaseId === 'installer-boot') {
      push('power-on', 'tracked', startMs, first.timeMs, 'provisioning started', first.opener)
    } else {
      push('untracked', 'untracked', startMs, first.timeMs, 'provisioning started', first.opener)
    }
  }

  for (let i = 0; i < anchors.length; i += 1) {
    const anchor = anchors[i]
    const isFinal = i + 1 === anchors.length
    if (!isFinal) {
      push(anchor.phaseId, 'tracked', anchor.timeMs, anchors[i + 1].timeMs, anchor.opener, anchors[i + 1].opener)
      continue
    }
    if (status === 'in-progress') {
      push(anchor.phaseId, 'live', anchor.timeMs, endMs, anchor.opener, 'now')
      continue
    }
    if (status === 'complete' && anchor.phaseId !== 'reboot-os') {
      // Completed without reboot evidence (legacy data): attribute the phase
      // only up to its last recorded activity and surface the rest honestly.
      const lastActivity = lastActivityEnd(events, anchor.timeMs, endMs)
      if (lastActivity !== undefined && endMs - lastActivity > TAIL_GAP_EPSILON_MS) {
        push(anchor.phaseId, 'tracked', anchor.timeMs, lastActivity, anchor.opener, 'last recorded event')
        push('untracked', 'untracked', lastActivity, endMs, 'last recorded event', endLabel(status))
        continue
      }
    }
    push(anchor.phaseId, 'tracked', anchor.timeMs, endMs, anchor.opener, endLabel(status), status === 'failed')
  }
  return segments
}

function lastActivityEnd(events: ResolvedEvent[], floorMs: number, ceilMs: number): number | undefined {
  let last: number | undefined
  for (const event of events) {
    const timeMs = event.endMs ?? event.startMs
    if (timeMs === undefined || timeMs <= floorMs || timeMs > ceilMs) continue
    if (last === undefined || timeMs > last) last = timeMs
  }
  return last
}

export function segmentAt(segments: PhaseSegment[], timeMs?: number): PhaseSegment | undefined {
  if (timeMs === undefined || segments.length === 0) return undefined
  for (const segment of segments) {
    if (timeMs >= segment.startMs && timeMs < segment.endMs) return segment
  }
  const lastSegment = segments[segments.length - 1]
  return timeMs >= lastSegment.endMs ? lastSegment : segments[0]
}
