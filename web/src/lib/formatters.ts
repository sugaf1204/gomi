import type { PowerConfig, PowerConfigResponse, PowerState, PowerType } from '../types'

export function formatDate(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString('en-US', { hour12: false })
}

export function formatMillis(ms?: number) {
  if (typeof ms !== 'number' || !Number.isFinite(ms)) return '-'
  const value = Math.max(0, ms)
  if (value < 1000) return `${Math.round(value)} ms`
  if (value < 60_000) return `${(value / 1000).toFixed(1)} s`
  const totalSeconds = Math.floor(value / 1000)
  if (value < 3_600_000) {
    return `${Math.floor(totalSeconds / 60)}m ${(totalSeconds % 60).toString().padStart(2, '0')}s`
  }
  const totalMinutes = Math.floor(totalSeconds / 60)
  return `${Math.floor(totalMinutes / 60)}h ${(totalMinutes % 60).toString().padStart(2, '0')}m`
}

export function formatRelativeOffset(ms?: number) {
  if (typeof ms !== 'number' || !Number.isFinite(ms)) return '-'
  const totalSeconds = Math.floor(Math.max(0, ms) / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) return `T+${hours}h${minutes.toString().padStart(2, '0')}m${seconds.toString().padStart(2, '0')}s`
  if (minutes > 0) return `T+${minutes}m${seconds.toString().padStart(2, '0')}s`
  return `T+${seconds}s`
}

// Fleet Rail chips are square and monospace-led (README → Typography: radius 0
// everywhere except the avatar; lowercase phase chip in mono 600 10.5px).
const CHIP_BASE = 'inline-flex items-center w-fit font-mono font-semibold text-[10.5px] lowercase px-[6px] py-[4px]'

export function phaseClass(phase?: string) {
  const base = CHIP_BASE
  switch ((phase ?? '').toLowerCase()) {
    case 'ready':
    case 'running':
    case 'succeeded':
    case 'registered':
      return `${base} bg-ok-bg text-ok`
    case 'pending':
    case 'provisioning':
    case 'creating':
    case 'deleting':
    case 'stopped':
    case 'migrating':
      return `${base} bg-warn-bg text-warn`
    case 'missing':
      return `${base} bg-neutral-bg text-ink-soft`
    default:
      return `${base} bg-error-bg text-error`
  }
}

export function powerMethodLabel(type?: PowerType) {
  switch (type) {
    case 'ipmi':
      return 'IPMI'
    case 'webhook':
      return 'Webhook API'
    case 'wol':
      return 'Wake-on-LAN'
    case 'manual':
      return 'Manual'
    default:
      return 'Unknown'
  }
}

function endpointHost(raw?: string) {
  if (!raw) return '-'
  try {
    return new URL(raw).host
  } catch {
    return raw
  }
}

export function formatPowerControlMethod(power?: PowerConfig | PowerConfigResponse) {
  if (!power || !power.type) {
    return 'No method configured'
  }
  if (power.type === 'ipmi' && power.ipmi) {
    return `IPMI - BMC ${power.ipmi.host}`
  }
  if (power.type === 'webhook' && power.webhook) {
    const onHost = endpointHost(power.webhook.powerOnURL)
    const offHost = endpointHost(power.webhook.powerOffURL)
    if (onHost === offHost) {
      return `Webhook - ${onHost}`
    }
    return `Webhook - on ${onHost} / off ${offHost}`
  }
  if (power.type === 'wol' && power.wol) {
    return `Wake-on-LAN - wake ${power.wol.wakeMAC}`
  }
  if (power.type === 'manual') {
    return 'Manual (no automation)'
  }
  return powerMethodLabel(power.type)
}

export function powerStateClass(state?: PowerState) {
  const base = CHIP_BASE
  switch (state) {
    case 'running':
      return `${base} bg-ok-bg text-ok`
    case 'stopped':
      return `${base} bg-error-bg text-error`
    default:
      return `${base} bg-neutral-bg text-ink-soft`
  }
}

export function powerStateLabel(state?: PowerState) {
  switch (state) {
    case 'running':
      return 'Powered On'
    case 'stopped':
      return 'Powered Off'
    default:
      return 'Unknown'
  }
}

/**
 * Compact "12s ago" / "4m ago" style age, for the header's sync indicator.
 * Returns an empty string when the timestamp is missing or unparseable, so
 * callers can omit the label entirely rather than render a placeholder.
 */
export function relativeTime(iso: string, nowMs: number): string {
  if (!iso) return ''
  const then = Date.parse(iso)
  if (Number.isNaN(then)) return ''

  const seconds = Math.max(0, Math.round((nowMs - then) / 1000))
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}
