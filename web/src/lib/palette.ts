import type { View } from '../app-types'
import { allNavItems } from './navigation'

export type PaletteEntry = {
  id: string
  label: string
  /** Shown right-aligned: what kind of thing this is. */
  kind: 'nav' | 'machine' | 'virtual machine' | 'subnet'
  view: View
  /** Object to select once the view is open, if any. */
  target?: string
}

export type PaletteSources = {
  machines: string[]
  virtualMachines: string[]
  subnets: string[]
}

/** Every jump-to destination: nav entries plus the named objects. */
export function buildPaletteEntries({ machines, virtualMachines, subnets }: PaletteSources): PaletteEntry[] {
  return [
    ...allNavItems().map((item) => ({
      id: `nav:${item.view}`,
      label: item.label,
      kind: 'nav' as const,
      view: item.view,
    })),
    ...machines.map((name) => ({
      id: `machine:${name}`, label: name, kind: 'machine' as const, view: 'machines' as View, target: name,
    })),
    ...virtualMachines.map((name) => ({
      id: `vm:${name}`, label: name, kind: 'virtual machine' as const, view: 'virtual-machines' as View, target: name,
    })),
    ...subnets.map((name) => ({
      id: `subnet:${name}`, label: name, kind: 'subnet' as const, view: 'network' as View, target: name,
    })),
  ]
}

/**
 * Subsequence match, the usual fuzzy-finder rule: every character of the query
 * must appear in order. Scores prefix matches highest, then earlier and tighter
 * matches, so typing "mach" surfaces "Machines" above "gpu-machine-01".
 */
export function fuzzyScore(query: string, text: string): number | null {
  if (!query) return 0
  const q = query.toLowerCase()
  const t = text.toLowerCase()
  if (t.startsWith(q)) return 1000 - t.length

  let score = 0
  let textIndex = 0
  let lastHit = -1
  for (const char of q) {
    const found = t.indexOf(char, textIndex)
    if (found === -1) return null
    score -= found - lastHit
    lastHit = found
    textIndex = found + 1
  }
  return score - t.length
}

/** Entries matching the query, best first. An empty query returns everything. */
export function filterPalette(entries: PaletteEntry[], query: string): PaletteEntry[] {
  return entries
    .map((entry) => ({ entry, score: fuzzyScore(query, entry.label) }))
    .filter((row): row is { entry: PaletteEntry; score: number } => row.score !== null)
    .sort((a, b) => b.score - a.score)
    .map((row) => row.entry)
}
