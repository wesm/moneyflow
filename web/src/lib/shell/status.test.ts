import { afterEach, describe, expect, it, vi } from 'vitest'

import { connectionState, status, writeProgress } from './status.svelte'
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

describe('status notice', () => {
  afterEach(() => {
    status.clear()
    vi.useRealTimers()
  })

  it('shows a notice for 4 seconds', () => {
    vi.useFakeTimers()
    status.notify('Open the group to delete its transactions.')
    expect(status.notice).toBe('Open the group to delete its transactions.')
    vi.advanceTimersByTime(3999)
    expect(status.notice).toBe('Open the group to delete its transactions.')
    vi.advanceTimersByTime(1)
    expect(status.notice).toBe('')
  })

  it('restarts the 4 seconds when a new notice replaces the last', () => {
    vi.useFakeTimers()
    status.notify('First notice')
    vi.advanceTimersByTime(3000)
    status.notify('Second notice')
    vi.advanceTimersByTime(3000)
    expect(status.notice).toBe('Second notice')
    vi.advanceTimersByTime(1000)
    expect(status.notice).toBe('')
  })

  it('clears at once, and a cleared notice does not end a later one early', () => {
    vi.useFakeTimers()
    status.notify('First notice')
    status.clear()
    expect(status.notice).toBe('')
    vi.advanceTimersByTime(2000)
    status.notify('Second notice')
    vi.advanceTimersByTime(3000)
    expect(status.notice).toBe('Second notice')
  })
})
