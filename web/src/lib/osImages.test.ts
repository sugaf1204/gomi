import { describe, expect, it } from 'vitest'
import { isWindowsImage, supportsDeploymentTarget, windowsFormDefaults } from './osImages'
import type { OSImage } from '../types'

const image: OSImage = {
  name: 'windows-11', osFamily: 'windows', osVersion: '11', arch: 'amd64',
  format: 'wim', source: 'upload', ready: true,
  manifest: { windows: { imageIndex: 1, winPE: { wimboot: 'wimboot', bcd: 'BCD', bootSDI: 'boot.sdi', bootWIM: 'boot.wim' } } },
}

describe('Windows deployment selection', () => {
  it('offers Windows WIM for both VM and Machine deployment', () => {
    expect(isWindowsImage(image)).toBe(true)
    expect(supportsDeploymentTarget(image, 'vm')).toBe(true)
    expect(supportsDeploymentTarget(image, 'baremetal')).toBe(true)
  })
  it('honors an explicit Windows VM-only deployment target', () => {
    const restricted: OSImage = { ...image, manifest: { ...image.manifest, capabilities: { deployTargets: ['vm'] } } }
    expect(supportsDeploymentTarget(restricted, 'vm')).toBe(true)
    expect(supportsDeploymentTarget(restricted, 'baremetal')).toBe(false)
  })
  it('requires Windows metadata for an implicit WIM deployment target', () => {
    expect(supportsDeploymentTarget({ ...image, manifest: undefined }, 'vm')).toBe(false)
    expect(supportsDeploymentTarget({ ...image, osFamily: 'debian' }, 'baremetal')).toBe(false)
  })
  it('does not offer a Windows qcow2 through Linux deployment fallbacks', () => {
    const invalid: OSImage = { ...image, format: 'qcow2', variant: 'cloud' }
    expect(supportsDeploymentTarget(invalid, 'vm')).toBe(false)
    expect(supportsDeploymentTarget(invalid, 'baremetal')).toBe(false)
  })
  it('clears Linux-only state when changing a configured form to Windows', () => {
    const previous = { cloudInitMode: 'existing', sshKeyRefs: ['key'], loginUserPassword: 'secret', ipAssignment: 'static', staticIP: '192.0.2.5' }
    const next = { ...previous, ...windowsFormDefaults() }
    expect(next.cloudInitMode).toBe('none')
    expect(next.sshKeyRefs).toEqual([])
    expect(next.loginUserPassword).toBe('')
    expect(next.loginUserPasswordTouched).toBe(true)
    expect(next.ipAssignment).toBe('dhcp')
    expect(next.staticIP).toBe('')
  })
  it('preserves Linux image selection rules', () => {
    const linux: OSImage = { ...image, osFamily: 'debian', format: 'qcow2', variant: 'cloud' }
    expect(supportsDeploymentTarget(linux, 'vm')).toBe(true)
    expect(supportsDeploymentTarget(linux, 'baremetal')).toBe(false)
  })
})
