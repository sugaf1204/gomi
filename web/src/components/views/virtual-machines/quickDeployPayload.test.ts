import { describe, expect, it } from 'vitest'
import { buildCreateVMPayload, buildVMNetworkPayload } from './useVirtualMachineOperations'
import { initialQuickDeployPreset } from './vmFormState'
import type { QuickDeployPreset } from './vmFormState'

// Quick Deploy used to build its API payload inline, which silently dropped
// advanced options, domain, and static IP. It now shares the create builders,
// so these assert the fields actually reach the payload.
function payloadFor(overrides: Partial<QuickDeployPreset>) {
  const preset: QuickDeployPreset = { ...initialQuickDeployPreset, name: 'devvm', osImageRef: 'ubuntu-24', ...overrides }
  return buildCreateVMPayload(preset, [], buildVMNetworkPayload(preset))
}

describe('quick deploy payload', () => {
  it('carries advanced options', () => {
    const payload = payloadFor({
      cpuMode: 'host-passthrough',
      diskDriver: 'scsi',
      diskFormat: 'qcow2',
      ioThreads: '4',
      netMultiqueue: '2',
      cpuPinning: '0:0,1:2'
    })

    expect(payload.advancedOptions).toEqual({
      cpuMode: 'host-passthrough',
      diskDriver: 'scsi',
      diskFormat: 'qcow2',
      ioThreads: 4,
      netMultiqueue: 2,
      cpuPinning: { 0: '0', 1: '2' }
    })
  })

  it('omits advanced options when none are set', () => {
    expect(payloadFor({})).not.toHaveProperty('advancedOptions')
  })

  it('carries the domain', () => {
    expect(payloadFor({ domain: 'example.local' }).domain).toBe('example.local')
  })

  it('omits a blank domain', () => {
    expect(payloadFor({ domain: '   ' })).not.toHaveProperty('domain')
  })

  it('carries a static IP and assignment', () => {
    const payload = payloadFor({ ipAssignment: 'static', staticIP: '192.168.1.50' })

    expect(payload.ipAssignment).toBe('static')
    expect(payload.network?.[0]?.ipAddress).toBe('192.168.1.50')
  })

  it('leaves the address unset for a DHCP preset', () => {
    const payload = payloadFor({ ipAssignment: 'dhcp', bridge: 'br0' })

    expect(payload).not.toHaveProperty('ipAssignment')
    expect(payload.network?.[0]?.ipAddress).toBeUndefined()
  })

  it('carries bridge and subnet into the network payload', () => {
    const payload = payloadFor({ bridge: 'br0', subnetRef: 'lab' })

    expect(payload.network?.[0]).toMatchObject({ name: 'default', bridge: 'br0', network: 'lab' })
    expect(payload.subnetRef).toBe('lab')
  })

  it('omits the network entirely when no network field is set', () => {
    expect(payloadFor({})).not.toHaveProperty('network')
  })

  it('carries resources and the login user', () => {
    const payload = payloadFor({
      cpuCores: '8',
      memoryMB: '16384',
      diskGB: '100',
      loginUserUsername: 'ubuntu',
      loginUserPassword: 'pw'
    })

    expect(payload.resources).toEqual({ cpuCores: 8, memoryMB: 16384, diskGB: 100 })
    expect(payload.loginUser).toEqual({ username: 'ubuntu', password: 'pw' })
  })
})
