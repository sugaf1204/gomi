import type { PhaseId } from '../../lib/deploy-phases'

export type PhaseColor = { fill: string; border: string }

// Phase fills resolve through CSS custom properties declared in styles.css, so
// the ramp swaps with the light/dark appearance the same way every other colour
// does. Band widths are data-derived and cannot be utility classes, which is
// why these are inline style values rather than Tailwind classes.
function phaseVar(phaseId: PhaseId, suffix: '' | '-running'): string {
  return `var(--phase-${phaseId}${suffix})`
}

function phaseLine(phaseId: PhaseId): string {
  return `var(--phase-${phaseId}-line)`
}

const PHASE_IDS: PhaseId[] = ['power-on', 'installer-boot', 'inventory-config', 'image-apply', 'reboot-os', 'untracked']

function buildRamp(suffix: '' | '-running'): Record<PhaseId, PhaseColor> {
  return Object.fromEntries(
    PHASE_IDS.map((id) => [id, { fill: phaseVar(id, suffix), border: phaseLine(id) }])
  ) as Record<PhaseId, PhaseColor>
}

/** Finished phases. */
export const PHASE_COLORS: Record<PhaseId, PhaseColor> = buildRamp('')

// Tone, not motion, distinguishes done / running / pending. A running phase is
// a tint of its own finished fill; a phase not yet reached is the palest
// neutral. Nothing pulses, sweeps or spins.
export const RUNNING_COLORS: Record<PhaseId, PhaseColor> = buildRamp('-running')

export const FAILED_COLOR: PhaseColor = { fill: 'var(--phase-failed)', border: 'var(--phase-failed-line)' }

/** Phases the attempt has not reached yet. */
export const NOT_STARTED_COLOR: PhaseColor = { fill: 'var(--color-track)', border: 'var(--color-line-soft)' }

export function phaseTone(phaseId: PhaseId, state: 'done' | 'running' | 'pending', failed = false): PhaseColor {
  if (failed) return FAILED_COLOR
  if (state === 'pending') return NOT_STARTED_COLOR
  return state === 'running' ? RUNNING_COLORS[phaseId] : PHASE_COLORS[phaseId]
}

export const UNTRACKED_HATCH =
  'repeating-linear-gradient(135deg, rgba(0,0,0,0.10) 0 4px, transparent 4px 8px)'

export function segmentColor(phaseId: PhaseId, failed: boolean): PhaseColor {
  return failed ? FAILED_COLOR : PHASE_COLORS[phaseId]
}
