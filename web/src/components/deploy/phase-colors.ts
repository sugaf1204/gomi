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

export const UNTRACKED_HATCH =
  'repeating-linear-gradient(135deg, rgba(0,0,0,0.10) 0 4px, transparent 4px 8px)'

export function segmentColor(phaseId: PhaseId, failed: boolean): PhaseColor {
  return failed ? FAILED_COLOR : PHASE_COLORS[phaseId]
}
