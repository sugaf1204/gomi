import { beforeAll, beforeEach, describe, expect, it } from 'vitest'
import {
  QUICK_DEPLOY_STORAGE_KEY,
  initialQuickDeployPreset,
  invalidVMConfigReason,
  readQuickDeployPreset,
  renderPresetTemplateName,
  writeQuickDeployPreset
} from './vmFormState'

// The preset helpers only need window + localStorage, so a minimal stub keeps
// these tests in the default node environment instead of pulling in jsdom.
function installStorageStub() {
  const store = new Map<string, string>()
  const storage = {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => void store.set(key, String(value)),
    removeItem: (key: string) => void store.delete(key),
    clear: () => store.clear()
  }
  Object.assign(globalThis, { window: globalThis, localStorage: storage })
}

beforeAll(installStorageStub)

function storePreset(value: unknown) {
  localStorage.setItem(QUICK_DEPLOY_STORAGE_KEY, JSON.stringify(value))
}

describe('readQuickDeployPreset', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('returns the initial preset when nothing is stored', () => {
    expect(readQuickDeployPreset()).toEqual(initialQuickDeployPreset)
  })

  it('returns the initial preset when the stored value is malformed', () => {
    localStorage.setItem(QUICK_DEPLOY_STORAGE_KEY, '{not json')
    expect(readQuickDeployPreset()).toEqual(initialQuickDeployPreset)
  })

  it('migrates a legacy multi-select cloudInitRefs preset to the first template', () => {
    storePreset({ name: 'dev', cloudInitRefs: ['base', 'extra', 'third'] })

    const preset = readQuickDeployPreset()

    expect(preset.cloudInitMode).toBe('existing')
    expect(preset.cloudInitExistingRef).toBe('base')
  })

  it('migrates an empty legacy cloudInitRefs array to none', () => {
    storePreset({ name: 'dev', cloudInitRefs: [] })

    const preset = readQuickDeployPreset()

    expect(preset.cloudInitMode).toBe('none')
    expect(preset.cloudInitExistingRef).toBe('')
  })

  it('ignores blank and non-string entries when migrating legacy refs', () => {
    storePreset({ name: 'dev', cloudInitRefs: [null, '  ', 'real', 7] })

    const preset = readQuickDeployPreset()

    expect(preset.cloudInitMode).toBe('existing')
    expect(preset.cloudInitExistingRef).toBe('real')
  })

  it('keeps an already-migrated cloudInitMode instead of re-migrating', () => {
    storePreset({ name: 'dev', cloudInitMode: 'create', cloudInitTemplateName: 'ci-{{ hostname }}', cloudInitRefs: ['stale'] })

    const preset = readQuickDeployPreset()

    expect(preset.cloudInitMode).toBe('create')
    expect(preset.cloudInitTemplateName).toBe('ci-{{ hostname }}')
  })

  it('restores neither the login password nor the cloud-init user-data', () => {
    storePreset({
      name: 'dev',
      loginUserUsername: 'ubuntu',
      loginUserPassword: 'sekrit',
      cloudInitMode: 'create',
      cloudInitUserData: '#cloud-config\nssh_authorized_keys:\n  - ssh-ed25519 AAAA'
    })

    const preset = readQuickDeployPreset()

    expect(preset.loginUserUsername).toBe('ubuntu')
    expect(preset.loginUserPassword).toBe('')
    expect(preset.cloudInitUserData).toBe('')
  })

  it('restores the advanced options that the preset now supports', () => {
    storePreset({
      name: 'dev',
      cpuMode: 'host-passthrough',
      diskDriver: 'scsi',
      ioThreads: '4',
      domain: 'example.local',
      ipAssignment: 'static',
      staticIP: '192.168.1.50'
    })

    const preset = readQuickDeployPreset()

    expect(preset.cpuMode).toBe('host-passthrough')
    expect(preset.diskDriver).toBe('scsi')
    expect(preset.ioThreads).toBe('4')
    expect(preset.domain).toBe('example.local')
    expect(preset.ipAssignment).toBe('static')
    expect(preset.staticIP).toBe('192.168.1.50')
  })

  it('normalizes a numeric stored count back to a string', () => {
    storePreset({ name: 'dev', count: 7 })

    expect(readQuickDeployPreset().count).toBe('7')
  })
})

describe('writeQuickDeployPreset', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('omits the login password and cloud-init user-data from storage', () => {
    writeQuickDeployPreset({
      ...initialQuickDeployPreset,
      name: 'dev',
      loginUserPassword: 'sekrit',
      cloudInitUserData: '#cloud-config\npassword: hunter2'
    })

    const raw = localStorage.getItem(QUICK_DEPLOY_STORAGE_KEY) ?? ''

    expect(raw).not.toContain('sekrit')
    expect(raw).not.toContain('hunter2')
    expect(JSON.parse(raw)).not.toHaveProperty('loginUserPassword')
    expect(JSON.parse(raw)).not.toHaveProperty('cloudInitUserData')
  })

  it('persists the cloud-init template name and mode so an existing preset restores', () => {
    writeQuickDeployPreset({
      ...initialQuickDeployPreset,
      name: 'dev',
      cloudInitMode: 'existing',
      cloudInitExistingRef: 'base-template'
    })

    const preset = readQuickDeployPreset()

    expect(preset.cloudInitMode).toBe('existing')
    expect(preset.cloudInitExistingRef).toBe('base-template')
  })

  it('clamps a non-positive count to 1', () => {
    writeQuickDeployPreset({ ...initialQuickDeployPreset, name: 'dev', count: '0' })

    expect(readQuickDeployPreset().count).toBe('1')
  })
})

describe('invalidVMConfigReason', () => {
  const valid = { ...initialQuickDeployPreset, name: 'devvm', osImageRef: 'ubuntu-24' }

  it('accepts a preset with default resources', () => {
    expect(invalidVMConfigReason(valid)).toBeUndefined()
  })

  it.each([
    ['cpuCores', 'CPU cores'],
    ['memoryMB', 'Memory (MB)'],
    ['diskGB', 'Disk (GB)']
  ] as const)('rejects a blank %s', (field, label) => {
    expect(invalidVMConfigReason({ ...valid, [field]: '' })).toBe(`${label} must be a positive number`)
  })

  it.each(['0', '-1', 'abc'] as const)('rejects cpuCores of %s', (value) => {
    expect(invalidVMConfigReason({ ...valid, cpuCores: value })).toBe('CPU cores must be a positive number')
  })

  it('rejects cpu pinning that targets a vcpu beyond cpuCores', () => {
    expect(invalidVMConfigReason({ ...valid, cpuCores: '2', cpuPinning: '2:4' }))
      .toBe('CPU pinning vcpu 2 is out of range [0, 2)')
  })

  it('accepts cpu pinning within range', () => {
    expect(invalidVMConfigReason({ ...valid, cpuCores: '4', cpuPinning: '0:0,1:2,3:6' })).toBeUndefined()
  })

  it('accepts an empty cpu pinning field', () => {
    expect(invalidVMConfigReason({ ...valid, cpuPinning: '' })).toBeUndefined()
  })

  // Negative values are silently dropped by buildAdvancedOptions and fractional
  // ones reach Go int fields, so both must be rejected before deploy.
  it.each([
    ['ioThreads', 'IO threads'],
    ['netMultiqueue', 'Net multiqueue']
  ] as const)('rejects a negative %s', (field, label) => {
    expect(invalidVMConfigReason({ ...valid, [field]: '-1' })).toBe(`${label} must be a whole number between 0 and 16`)
  })

  it.each(['2.5', '17', '-1'] as const)('rejects ioThreads of %s', (value) => {
    expect(invalidVMConfigReason({ ...valid, ioThreads: value })).toBe('IO threads must be a whole number between 0 and 16')
  })

  it.each(['', '0', '4', '16'] as const)('accepts ioThreads of %s', (value) => {
    expect(invalidVMConfigReason({ ...valid, ioThreads: value })).toBeUndefined()
  })

  it('rejects an unparsable static IP', () => {
    expect(invalidVMConfigReason({ ...valid, ipAssignment: 'static', staticIP: '192.168.1.999' }))
      .toBe('Static IP must be a valid IPv4 or IPv6 address')
  })

  it.each(['192.168.1.1', '10.0.0.255', '::1', 'fe80::1', '::ffff:192.168.1.1'] as const)('accepts static IP %s', (ip) => {
    expect(invalidVMConfigReason({ ...valid, ipAssignment: 'static', staticIP: ip })).toBeUndefined()
  })

  it.each(['192.168.1', '192.168.1.1.1', 'not-an-ip', '1::2::3'] as const)('rejects static IP %s', (ip) => {
    expect(invalidVMConfigReason({ ...valid, ipAssignment: 'static', staticIP: ip }))
      .toBe('Static IP must be a valid IPv4 or IPv6 address')
  })

  it('ignores the static IP when assignment is dhcp', () => {
    expect(invalidVMConfigReason({ ...valid, ipAssignment: 'dhcp', staticIP: 'nonsense' })).toBeUndefined()
  })
})

describe('renderPresetTemplateName', () => {
  it('substitutes the hostname variable', () => {
    expect(renderPresetTemplateName('ci-{{ hostname }}', 'devvm-3')).toBe('ci-devvm-3')
  })

  it('accepts the variable without surrounding spaces', () => {
    expect(renderPresetTemplateName('ci-{{hostname}}', 'devvm-3')).toBe('ci-devvm-3')
  })

  it('substitutes every occurrence', () => {
    expect(renderPresetTemplateName('{{ hostname }}-ci-{{ hostname }}', 'web1')).toBe('web1-ci-web1')
  })

  it('passes through a name with no variables', () => {
    expect(renderPresetTemplateName('static-template', 'devvm-3')).toBe('static-template')
  })

  it('leaves unknown variables intact so a typo degrades to a literal name', () => {
    expect(renderPresetTemplateName('ci-{{ hostnaem }}', 'devvm-3')).toBe('ci-{{ hostnaem }}')
  })

  // The create and redeploy dialogs resolve templates without a hostname.
  // Substituting '' there would collapse the name and, because the template
  // store upserts on name conflict, silently overwrite an unrelated template.
  it('leaves the placeholder intact when no hostname is supplied', () => {
    expect(renderPresetTemplateName('ci-{{ hostname }}', '')).toBe('ci-{{ hostname }}')
  })

  it('does not collapse a bare placeholder to an empty name', () => {
    expect(renderPresetTemplateName('{{ hostname }}', '')).toBe('{{ hostname }}')
  })

  it('still passes through a literal name when no hostname is supplied', () => {
    expect(renderPresetTemplateName('static-template', '')).toBe('static-template')
  })

  // VM names are only checked for non-emptiness, so a name containing a
  // replacement token must be inserted literally rather than expanded.
  it.each([
    ['vm$&-1', 'ci-vm$&-1'],
    ["vm$'-1", "ci-vm$'-1"],
    ['vm$`-1', 'ci-vm$`-1'],
    ['vm$$-1', 'ci-vm$$-1'],
    ['vm$1-1', 'ci-vm$1-1']
  ])('inserts %s literally', (hostname, expected) => {
    expect(renderPresetTemplateName('ci-{{ hostname }}', hostname)).toBe(expected)
  })
})
