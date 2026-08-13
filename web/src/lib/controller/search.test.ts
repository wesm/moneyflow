import { afterEach, describe, expect, it, vi } from 'vitest'

import { createSearchCoordinator, type SearchHost } from './search'

describe('search coordinator', () => {
  afterEach(() => vi.useRealTimers())

  it('debounces previews and commits one accepted projection', async () => {
    vi.useFakeTimers()
    const host = fakeHost()
    const search = createSearchCoordinator(host)

    search.input('coffee')
    await vi.advanceTimersByTimeAsync(149)
    expect(host.previewSearch).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(host.previewSearch).toHaveBeenCalledWith('coffee')

    await search.commit()
    expect(host.commitSearch).toHaveBeenCalledTimes(1)
    expect(host.commitSearch).toHaveBeenCalledWith(host.snapshot)
  })

  it('retains the last accepted view after invalid input', async () => {
    vi.useFakeTimers()
    const host = fakeHost({ previewSearch: vi.fn(async () => false) })
    const search = createSearchCoordinator(host)

    search.input('[')
    await vi.advanceTimersByTimeAsync(150)

    expect(search.error).toBe('That search expression is invalid.')
    expect(host.commitSearch).not.toHaveBeenCalled()
  })

  it('cancels pending work and restores the complete opening snapshot', async () => {
    vi.useFakeTimers()
    const host = fakeHost()
    const search = createSearchCoordinator(host)

    search.input('not sent')
    search.cancel()
    await vi.runAllTimersAsync()

    expect(host.previewSearch).not.toHaveBeenCalled()
    expect(host.restoreSearch).toHaveBeenCalledWith(host.snapshot)
  })

  it('rejects text beyond the server UTF-8 limit before requesting', async () => {
    vi.useFakeTimers()
    const host = fakeHost()
    const search = createSearchCoordinator(host)

    search.input('é'.repeat(1025))
    await vi.runAllTimersAsync()

    expect(search.error).toBe('Search text must be at most 2048 bytes.')
    expect(host.previewSearch).not.toHaveBeenCalled()
  })
})

function fakeHost(overrides: Partial<SearchHost<object>> = {}) {
  const snapshot = { query: 'v=1', selection: 'opaque', cursorIndex: 7, scrollTop: 120 }
  return {
    snapshot,
    beginSearch: vi.fn(() => snapshot),
    previewSearch: vi.fn(async () => true),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
    ...overrides,
  }
}
