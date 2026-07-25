import { describe, expect, it } from 'vitest'
import { relativeTime } from './formatters'

const NOW = Date.parse('2026-07-26T12:00:00.000Z')

describe('relativeTime', () => {
  it('returns an empty string for a missing or unparseable timestamp', () => {
    expect(relativeTime('', NOW)).toBe('')
    expect(relativeTime('not-a-date', NOW)).toBe('')
  })

  it('reports seconds under a minute', () => {
    expect(relativeTime('2026-07-26T11:59:48.000Z', NOW)).toBe('12s ago')
  })

  it('rolls up to minutes, hours and days', () => {
    expect(relativeTime('2026-07-26T11:56:00.000Z', NOW)).toBe('4m ago')
    expect(relativeTime('2026-07-26T09:00:00.000Z', NOW)).toBe('3h ago')
    expect(relativeTime('2026-07-24T12:00:00.000Z', NOW)).toBe('2d ago')
  })

  it('clamps future timestamps to zero rather than showing a negative age', () => {
    expect(relativeTime('2026-07-26T12:00:30.000Z', NOW)).toBe('0s ago')
  })
})
