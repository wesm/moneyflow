import { afterEach, describe, expect, it, vi } from 'vitest'

import { connectionState, status, writeProgress } from './status.svelte'
import type { ProviderWriteStatus } from '../api/client'

describe('connectionState', () => {
  it('reports offline for a network error, even while a request runs', () => {
    expect(
      connectionState({ kind: 'request', code: 'network_error', detail: 'Unreachable.' }, true),
    ).toBe('offline')
  })

  it('reports updating while a request runs', () => {
    expect(connectionState(undefined, true)).toBe('updating')
  })

  it('reports failed after the server refused or could not answer the last request', () => {
    const refused = { kind: 'request' as const, code: 'store_busy', detail: 'Busy.' }
    expect(connectionState(refused, false)).toBe('failed')
    expect(
      connectionState({ kind: 'invalid-view', code: 'invalid_view_state', detail: '' }, false),
    ).toBe('failed')
    expect(connectionState(refused, true)).toBe('updating')
  })

  it('reports connected without a problem', () => {
    expect(connectionState(undefined, false)).toBe('connected')
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

describe('status notice', () => {
  afterEach(() => vi.useRealTimers())

  it('keeps a notice until a newer one replaces it', () => {
    vi.useFakeTimers()
    status.notify('Refresh complete: 6 imported, 0 removed.')
    vi.advanceTimersByTime(60_000)
    expect(status.notice).toBe('Refresh complete: 6 imported, 0 removed.')
    status.notify('Open the group to delete its transactions.')
    expect(status.notice).toBe('Open the group to delete its transactions.')
  })
})
