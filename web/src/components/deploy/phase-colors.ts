import type { PhaseId } from '../../lib/deploy-phases'

export type PhaseColor = { fill: string; border: string }

// Muted fills with darker borders, following the app's existing status
// palette family (see phaseClass/powerStateClass in lib/formatters.ts).
export const PHASE_COLORS: Record<PhaseId, PhaseColor> = {
  'power-on': { fill: '#c8d0e0', border: '#8090b0' },
  'installer-boot': { fill: '#d8cce8', border: '#9a86c0' },
  'inventory-config': { fill: '#f8e6cc', border: '#c09a54' },
  'image-apply': { fill: '#a0d8c0', border: '#3a9a6e' },
  'reboot-os': { fill: '#aad4e0', border: '#5a94ac' },
  untracked: { fill: '#e4e0da', border: '#b0aca4' },
}

export const FAILED_COLOR: PhaseColor = { fill: '#e8a0a0', border: '#c05050' }

// Tone, not motion, distinguishes done / running / pending. A running phase is
// a paler tint of its own finished fill; a phase not yet reached is the palest
// neutral. Nothing pulses, sweeps or spins.
export const RUNNING_COLORS: Record<PhaseId, PhaseColor> = {
  'power-on': { fill: '#e0e5ee', border: '#8090b0' },
  'installer-boot': { fill: '#ebe4f3', border: '#9a86c0' },
  'inventory-config': { fill: '#fcf2e2', border: '#c09a54' },
  'image-apply': { fill: '#c9e8da', border: '#3a9a6e' },
  'reboot-os': { fill: '#d3e7ee', border: '#5a94ac' },
  untracked: { fill: '#f0eeea', border: '#b0aca4' },
}

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
