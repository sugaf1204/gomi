import { useEffect, useState } from 'react'
import { relativeTime } from '../lib/formatters'

/**
 * Keeps a "12s ago" label current by re-reading the clock on an interval.
 * The value only changes tone/text on re-render — nothing animates, matching
 * the design's no-motion rule.
 */
export function useRelativeTime(iso: string, intervalMs = 5000): string {
  const [nowMs, setNowMs] = useState(() => Date.now())

  useEffect(() => {
    const timer = setInterval(() => setNowMs(Date.now()), intervalMs)
    return () => clearInterval(timer)
  }, [intervalMs])

  return relativeTime(iso, nowMs)
}
