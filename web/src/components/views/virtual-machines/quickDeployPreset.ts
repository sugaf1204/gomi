import { initialQuickDeployPreset, QUICK_DEPLOY_STORAGE_KEY } from './vmFormState'
import type { QuickDeployPreset, VMConfigForm } from './vmFormState'

type StoredQuickDeployPreset = Partial<QuickDeployPreset> & {
  count?: string | number
  // Legacy shape: the preset used to select multiple Cloud-Init templates.
  cloudInitRefs?: unknown
}

// Legacy presets selected multiple Cloud-Init templates. The unified form models
// a single template, so the first entry wins and the rest are dropped.
function migrateCloudInitSelection(parsed: StoredQuickDeployPreset) {
  if (parsed.cloudInitMode) {
    return {
      cloudInitMode: parsed.cloudInitMode,
      cloudInitExistingRef: parsed.cloudInitExistingRef ?? ''
    }
  }
  const legacyRefs = Array.isArray(parsed.cloudInitRefs)
    ? parsed.cloudInitRefs.filter((ref): ref is string => typeof ref === 'string' && ref.trim().length > 0)
    : []
  if (legacyRefs.length === 0) {
    return { cloudInitMode: 'none' as const, cloudInitExistingRef: '' }
  }
  return { cloudInitMode: 'existing' as const, cloudInitExistingRef: legacyRefs[0] }
}

export function readQuickDeployPreset(): QuickDeployPreset {
  if (typeof window === 'undefined') return initialQuickDeployPreset
  try {
    const raw = localStorage.getItem(QUICK_DEPLOY_STORAGE_KEY)
    if (!raw) return initialQuickDeployPreset
    const parsed = JSON.parse(raw) as StoredQuickDeployPreset
    return {
      ...initialQuickDeployPreset,
      ...parsed,
      name: typeof parsed.name === 'string' ? parsed.name : '',
      count: String(parsed.count ?? '1'),
      ...migrateCloudInitSelection(parsed),
      // Secrets are never persisted; see writeQuickDeployPreset.
      loginUserPassword: '',
      loginUserPasswordTouched: false,
      cloudInitUserData: '',
      sshKeyRefs: Array.isArray(parsed.sshKeyRefs) ? parsed.sshKeyRefs.filter((ref): ref is string => typeof ref === 'string') : []
    }
  } catch {
    return initialQuickDeployPreset
  }
}

// Cloud-Init user-data routinely carries SSH keys and tokens, so it is dropped
// alongside the login password rather than written to localStorage.
export function writeQuickDeployPreset(preset: QuickDeployPreset) {
  if (typeof window === 'undefined') return
  try {
    const { loginUserPassword: _password, cloudInitUserData: _userData, ...safePreset } = preset
    localStorage.setItem(QUICK_DEPLOY_STORAGE_KEY, JSON.stringify({
      ...safePreset,
      count: Math.max(1, Number(preset.count) || 1)
    }))
  } catch {
    // ignore localStorage access errors
  }
}

// The inline Cloud-Init template name is a GOMI resource identifier, so GOMI
// resolves it at deploy time. The user-data body itself is left untouched:
// cloud-init renders its own Jinja on the target from instance-data.
//
// Callers with no concrete hostname (the create and redeploy dialogs, which
// name each VM per iteration) leave the placeholder intact. Substituting an
// empty string would collapse "ci-{{ hostname }}" to "ci-", and the template
// store upserts on name conflict, so that would silently overwrite a shared
// template instead of creating the intended one.
export function renderPresetTemplateName(rawName: string, hostname: string): string {
  if (!hostname) return rawName
  // A replacement callback, not a replacement string: VM names are only checked
  // for non-emptiness, so a name containing $&, $` or $' would otherwise expand
  // as a replacement token instead of being inserted literally.
  return rawName.replace(/\{\{\s*hostname\s*\}\}/g, () => hostname)
}

// The name the next deploy will claim. `count` is the suffix rather than a
// batch size: each deploy takes the current number and increments it.
export function quickDeployVMName(preset: QuickDeployPreset): string {
  return `${preset.name.trim()}-${Math.max(1, Number(preset.count) || 1)}`
}

// Whether the preset can be deployed as-is. The header's New VM button acts on
// the stored preset without opening a dialog first, so every required field has
// to be checked here rather than by form validation. Only `name` is read off the
// image list, so any {name} shape stands in for OSImage[].
export function quickDeployPresetReady(preset: QuickDeployPreset, vmOSImages: { name: string }[]): boolean {
  if (!preset.name.trim()) return false
  if ((Number(preset.count) || 0) < 1) return false
  if (!preset.osImageRef.trim()) return false
  if (preset.ipAssignment === 'static' && !preset.staticIP.trim()) return false
  if (preset.cloudInitMode === 'create' && !(preset.cloudInitTemplateName.trim() && preset.cloudInitUserData.trim())) return false
  if (invalidVMConfigReason(preset)) return false
  // Checked against the VM-capable subset the dialog offers, so a preset naming
  // a baremetal-only image cannot slip through.
  return vmOSImages.some((img) => img.name === preset.osImageRef)
}

// The Create dialog is a real <form>, so the browser enforces required/min on
// these inputs before submit. Quick Deploy's button sits outside a form, so
// constraint validation never runs and these must be checked explicitly.
// Mirrors the rules the API applies in internal/vm/validate.go, so a preset
// fails here rather than after an inline Cloud-Init template has been created.
export function invalidVMConfigReason(formState: VMConfigForm): string | undefined {
  // Go decodes these into int/int64, so a fractional value fails to decode.
  const positiveFields = [
    { label: 'CPU cores', value: formState.cpuCores },
    { label: 'Memory (MB)', value: formState.memoryMB },
    { label: 'Disk (GB)', value: formState.diskGB }
  ]
  for (const field of positiveFields) {
    const parsed = Number(field.value)
    if (!field.value.trim() || !Number.isInteger(parsed) || parsed <= 0) {
      return `${field.label} must be a positive whole number`
    }
  }

  // Optional, but when present must be an integer within the input's min/max.
  // A negative value is silently dropped by buildAdvancedOptions, and a
  // fractional one reaches a Go int field and fails to decode.
  const boundedFields = [
    { label: 'IO threads', value: formState.ioThreads },
    { label: 'Net multiqueue', value: formState.netMultiqueue }
  ]
  for (const field of boundedFields) {
    if (!field.value.trim()) continue
    const parsed = Number(field.value)
    if (!Number.isInteger(parsed) || parsed < 0 || parsed > 16) {
      return `${field.label} must be a whole number between 0 and 16`
    }
  }

  // Validated against the raw text, not parseCPUPinning's output: that parser
  // silently drops malformed segments, so a typo would otherwise be discarded
  // without telling anyone, and a fractional index would survive as a
  // non-integer key that Go's map[int]string cannot decode.
  const cpuCores = Number(formState.cpuCores)
  const pinningText = formState.cpuPinning.trim()
  if (pinningText) {
    for (const segment of pinningText.split(',')) {
      const [vcpu, cpuset] = segment.split(':').map((part) => part.trim())
      if (segment.split(':').length !== 2 || !/^\d+$/.test(vcpu ?? '') || !cpuset) {
        return `CPU pinning "${segment.trim()}" is invalid (expected vcpu:cpuset, e.g. 0:0,1:2)`
      }
      if (Number(vcpu) >= cpuCores) {
        return `CPU pinning vcpu ${Number(vcpu)} is out of range [0, ${cpuCores})`
      }
      // The backend validates only the vcpu key; the cpuset value is written
      // straight into libvirt's cpuset XML attribute, so an invalid set is not
      // caught until domain definition fails.
      if (!isValidCPUSet(cpuset)) {
        return `CPU pinning cpuset "${cpuset}" is invalid (expected a CPU list such as 0, 1-4 or 1-4,^3,6)`
      }
    }
  }

  // Mirrors resource.ValidateIPAssignment, which rejects an unparsable address.
  if (formState.ipAssignment === 'static' && !isParsableIP(formState.staticIP.trim())) {
    return 'Static IP must be a valid IPv4 or IPv6 address'
  }

  // Mirrors linuxUsernamePattern in internal/vm/validate.go. An empty username
  // is valid: it simply means no login user is configured.
  const username = formState.loginUserUsername.trim()
  if (username && !/^[a-z_][a-z0-9_-]{0,31}$/.test(username)) {
    return 'Login username must be lowercase, start with a letter or underscore, and be at most 32 characters'
  }
  return undefined
}

// libvirt's cpuset attribute takes a comma-separated CPU list where each
// element is a single CPU, a range, or a caret exclusion (e.g. "1-4,^3,6").
// This form field already uses commas to separate vcpu:cpuset pairs, so a
// single pin can only carry one element; a bare "^3" is rejected here rather
// than being silently misparsed as a separate pin.
function isValidCPUSet(raw: string): boolean {
  const range = raw.match(/^(\d+)-(\d+)$/)
  if (range) return Number(range[1]) <= Number(range[2])
  return /^\d+$/.test(raw)
}

// Accepts the same addresses as Go's net.ParseIP: dotted-quad IPv4 with each
// octet in 0-255, or an IPv6 address (optionally with an embedded IPv4 tail).
function isParsableIP(raw: string): boolean {
  if (!raw) return false
  if (raw.includes(':')) return isParsableIPv6(raw)
  return isParsableIPv4(raw)
}

function isParsableIPv4(raw: string): boolean {
  const octets = raw.split('.')
  if (octets.length !== 4) return false
  // Go's net.ParseIP rejects leading zeros in dotted-decimal octets, so "0" is
  // valid but "01" and "001" are not.
  return octets.every((octet) => /^(0|[1-9]\d{0,2})$/.test(octet) && Number(octet) <= 255)
}

function isParsableIPv6(raw: string): boolean {
  const compressedParts = raw.split('::')
  if (compressedParts.length > 2) return false
  const groups = compressedParts.map((part) => (part ? part.split(':') : []))
  const tail = groups[groups.length - 1]
  // An embedded IPv4 tail (e.g. ::ffff:192.168.1.1) occupies two groups.
  let embeddedIPv4 = 0
  if (tail.length > 0 && tail[tail.length - 1].includes('.')) {
    if (!isParsableIPv4(tail[tail.length - 1])) return false
    tail.pop()
    embeddedIPv4 = 2
  }
  const total = groups.reduce((sum, part) => sum + part.length, 0) + embeddedIPv4
  if (groups.some((part) => part.some((hextet) => !/^[0-9a-fA-F]{1,4}$/.test(hextet)))) return false
  return compressedParts.length === 2 ? total <= 7 : total === 8
}
