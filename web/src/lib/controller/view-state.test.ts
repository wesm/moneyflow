import { afterEach, describe, expect, it, vi } from 'vitest'

import { historyViewState } from './view-state'

describe('historyViewState', () => {
  afterEach(() => history.replaceState(null, '', '/'))

  it('writes each query to the application path and reads the entry back', () => {
    history.replaceState(null, '', '/moneyflow/p/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/')
    const viewState = historyViewState('/moneyflow/p/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/')
    const length = history.length

    viewState.push({ step: 1 }, 'group=category&v=1')
    expect(location.pathname + location.search).toBe(
      '/moneyflow/p/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/?group=category&v=1',
    )
    expect(viewState.state).toEqual({ step: 1 })
    expect(viewState.query).toBe('group=category&v=1')
    expect(history.length).toBe(length + 1)

    viewState.replace(null, '')
    expect(location.pathname + location.search).toBe(
      '/moneyflow/p/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/',
    )
    expect(viewState.state).toBeNull()
    expect(viewState.query).toBe('')
    expect(history.length).toBe(length + 1)
  })

  it('calls restore on back and forward until unsubscribed', () => {
    const restore = vi.fn()
    const unsubscribe = historyViewState('/').listen(restore)

    dispatchEvent(new PopStateEvent('popstate', { state: null }))
    unsubscribe()
    dispatchEvent(new PopStateEvent('popstate', { state: null }))

    expect(restore).toHaveBeenCalledTimes(1)
  })
})
