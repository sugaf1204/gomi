import { describe, expect, it } from 'vitest'
import { resourceHealth } from './status'
import type { ClusterAPIResource } from './types'

const resource: ClusterAPIResource = {
  resource: 'clusters', apiVersion: 'cluster.x-k8s.io/v1beta2', namespace: 'lab', name: 'demo',
  uid: 'uid', resourceVersion: '7', generation: 2, paused: false, deleting: false,
  topology: false, autoscaled: false, conditions: [],
}

describe('Cluster API health presentation', () => {
  it('keeps missing and unknown readiness neutral', () => {
    expect(resourceHealth(resource)).toEqual({ label: 'Unknown', tone: 'neutral' })
    expect(resourceHealth({ ...resource, conditions: [{ type: 'Ready', status: 'Unknown' }] })).toMatchObject({ label: 'Unknown', tone: 'neutral' })
  })
  it('surfaces the negative readiness reason without treating all false conditions as errors', () => {
    expect(resourceHealth({ ...resource, conditions: [{ type: 'Available', status: 'False', reason: 'WaitingForMachines', message: 'Waiting for a node' }] }))
      .toEqual({ label: 'Not ready', tone: 'warn', reason: 'WaitingForMachines', message: 'Waiting for a node' })
    expect(resourceHealth({ ...resource, conditions: [{ type: 'ScalingUp', status: 'False' }, { type: 'Ready', status: 'True' }] }))
      .toEqual({ label: 'Ready', tone: 'ok' })
  })
  it('does not present a prior generation as current readiness', () => {
    expect(resourceHealth({ ...resource, conditions: [{ type: 'Available', status: 'True', observedGeneration: 1 }] }))
      .toMatchObject({ label: 'Reconciling', tone: 'warn' })
  })
  it('prioritizes deletion and pause over old readiness', () => {
    const ready = { ...resource, conditions: [{ type: 'Ready', status: 'True' }] }
    expect(resourceHealth({ ...ready, deleting: true })).toEqual({ label: 'Deleting', tone: 'warn' })
    expect(resourceHealth({ ...ready, paused: true })).toEqual({ label: 'Paused', tone: 'neutral' })
  })
  it('uses an error tone for an explicit failure phase', () => {
    expect(resourceHealth({ ...resource, phase: 'Failed' })).toMatchObject({ label: 'Failed', tone: 'error' })
  })
})
