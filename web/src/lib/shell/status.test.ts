import { describe, expect, it } from 'vitest'

import { connectionState, writeProgress } from './status'
import type { ProviderWriteStatus } from '../api/client'

describe('connectionState', () => {
  it('reports offline for a network error, even while a request runs', () => {
    expect(connectionState({ kind: 'request', code: 'network_error' }, true)).toBe('offline')
  })

  it('reports updating while a request runs', () => {
    expect(connectionState(undefined, true)).toBe('updating')
  })

  it('reports connected otherwise, including after a non-network problem', () => {
    expect(connectionState(undefined, false)).toBe('connected')
    expect(connectionState({ kind: 'invalid-view', code: 'invalid_query' }, false)).toBe(
      'connected',
    )
  })
})

describe('writeProgress', () => {
  it('reports completed and total steps while a write has a phase', () => {
    const status = { phase: 'writing', completed: 2, total: 5 } as ProviderWriteStatus
    expect(writeProgress(status)).toEqual({ completed: 2, total: 5 })
  })

  it('reports nothing without a status or a phase', () => {
    expect(writeProgress(undefined)).toBeUndefined()
    expect(writeProgress({ completed: 0, total: 0 } as ProviderWriteStatus)).toBeUndefined()
  })
})
