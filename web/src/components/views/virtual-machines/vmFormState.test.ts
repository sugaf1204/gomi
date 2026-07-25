import { beforeAll, beforeEach, describe, expect, it } from 'vitest'
import {
  QUICK_DEPLOY_STORAGE_KEY,
  initialQuickDeployPreset,
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
})
