import type { PowerConfig, PowerConfigResponse, PowerState, PowerType } from '../types'

export function formatDate(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString('en-US', { hour12: false })
}

export function phaseClass(phase?: string) {
  const base = 'inline-flex items-center w-fit font-ui rounded-full text-[0.71rem] font-semibold px-2 py-0.5'
  switch ((phase ?? '').toLowerCase()) {
    case 'ready':
    case 'running':
    case 'succeeded':
    case 'registered':
      return `${base} bg-[#d9f1e5] text-ok`
    case 'pending':
    case 'provisioning':
    case 'creating':
    case 'deleting':
    case 'stopped':
    case 'migrating':
      return `${base} bg-[#f8e6cc] text-warn`
    default:
      return `${base} bg-[#f6dada] text-error`
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
  const base = 'inline-flex items-center w-fit font-ui rounded-full text-[0.68rem] font-semibold px-2 py-0.5'
  switch (state) {
    case 'running':
      return `${base} bg-[#d9f1e5] text-ok`
    case 'stopped':
      return `${base} bg-[#f6dada] text-error`
    default:
      return `${base} bg-[#e8e4df] text-ink-soft`
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
