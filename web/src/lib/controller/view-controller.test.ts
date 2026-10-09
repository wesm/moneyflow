import { beforeEach, describe, expect, it, vi } from 'vitest'

import { MoneyflowProblem, type MoneyflowClient, type ViewProjection } from '../api/client'
import { createViewController } from './view-controller.svelte'
import type { SelectionValue } from '../api/client'
import type { MoneyflowHistoryState } from './history'

describe('browser view controller', () => {
  beforeEach(() => history.replaceState(null, '', '/moneyflow/?hidden=1&v=1'))

  it('hydrates, canonicalizes by replacement, announces reset, and prefetches adjacent rows', async () => {
    const calls: number[] = []
    const client = clientWith({
      view: async (body) => {
        calls.push(body.window.offset)
        const result = projection('v=1', body.window.offset, 450)
        if (body.window.offset === 0) {
          result.warnings = [{ code: 'selection_reset', detail: 'Selection reset.' }]
        }
        return result
      },
    })
    const controller = controllerFor(client)

    await controller.hydrate()
    await vi.waitFor(() => expect(calls).toContain(200))

    expect(controller.projection?.canonical_query).toBe('v=1')
    expect(controller.cursorIndex).toBe(0)
    expect(controller.cursorIdentity).toBe('row-0')
    expect(controller.announcement).toBe('Selection reset.')
    expect(window.location.pathname + window.location.search).toBe('/moneyflow/?v=1')
    expect((history.state as MoneyflowHistoryState).owner).toBe('moneyflow-web-v1')
  })

  describe('first-load currency', () => {
    const currencies = (counts: Array<[string, number]>) =>
      counts.map(([currency, count]) => ({
        currency,
        scale: 2,
        count,
        first: '2024-01-01',
        latest: '2024-12-31',
      }))

    it('opens in the currency with the most transactions and replaces the history entry', async () => {
      const pushes = vi.spyOn(history, 'pushState')
      pushes.mockClear()
      const length = history.length
      const client = clientWith({
        view: vi.fn(async (body) => ({
          ...projection('v=1', body.window.offset, 3),
          currencies: currencies([
            ['USD', 3],
            ['EUR', 9],
          ]),
        })),
        transition: vi.fn(async (body) => ({
          ...projection('cur=EUR%3A2&v=1', body.window.offset, 3),
          view: { ...projection('v=1', 0, 3).view, currency: { currency: 'EUR', scale: 2 } },
        })),
      })
      const controller = controllerFor(client)

      await controller.hydrate()

      expect(client.transition).toHaveBeenCalledTimes(1)
      expect(client.transition).toHaveBeenCalledWith(
        expect.objectContaining({
          action: 'view.set-currency',
          currency: { currency: 'EUR', scale: 2 },
        }),
        expect.any(AbortSignal),
      )
      expect(controller.projection?.canonical_query).toBe('cur=EUR%3A2&v=1')
      expect(pushes).not.toHaveBeenCalled()
      expect(history.length).toBe(length)
      expect(window.location.search).toBe('?cur=EUR%3A2&v=1')
    })

    it('picks the first currency listed when counts tie', async () => {
      const client = clientWith({
        view: vi.fn(async (body) => ({
          ...projection('v=1', body.window.offset, 3),
          currencies: currencies([
            ['USD', 4],
            ['EUR', 4],
          ]),
        })),
      })
      await controllerFor(client).hydrate()

      expect(client.transition).toHaveBeenCalledWith(
        expect.objectContaining({ currency: { currency: 'USD', scale: 2 } }),
        expect.any(AbortSignal),
      )
    })

    it('sends nothing when the view already has a currency', async () => {
      const client = clientWith({
        view: vi.fn(async (body) => {
          const result = projection('cur=EUR%3A2&v=1', body.window.offset, 3)
          return {
            ...result,
            view: { ...result.view, currency: { currency: 'EUR', scale: 2 } },
            currencies: currencies([['USD', 3]]),
          }
        }),
      })
      await controllerFor(client).hydrate()

      expect(client.transition).not.toHaveBeenCalled()
    })

    it('sends nothing when the profile has no currencies', async () => {
      const client = clientWith()
      await controllerFor(client).hydrate()

      expect(client.transition).not.toHaveBeenCalled()
    })
  })

  it('pushes analytical transitions and replaces selection-only transitions', async () => {
    const pushes = vi.spyOn(history, 'pushState')
    const replacements = vi.spyOn(history, 'replaceState')
    const client = clientWith({
      transition: async (body) =>
        projection(body.action === 'view.cycle-grouping' ? 'v=1&group=category' : body.query, 0, 3),
    })
    const controller = controllerFor(client)
    await controller.hydrate()
    pushes.mockClear()
    replacements.mockClear()

    await controller.apply({ action: 'view.cycle-grouping' })
    expect(pushes).toHaveBeenCalledTimes(1)
    expect(window.location.search).toBe('?v=1&group=category')

    await controller.apply({
      action: 'selection.toggle',
      target: { kind: 'transaction', identity: 'row-0' },
    })
    expect(pushes).toHaveBeenCalledTimes(1)
    expect(replacements).toHaveBeenCalled()
  })

  it('forwards the dimension, currency, sort field, period, and depth to the transition', async () => {
    const client = clientWith()
    const controller = controllerFor(client)
    await controller.hydrate()
    const requests = [
      { action: 'view.set-grouping', dimension: 'category' },
      { action: 'view.set-currency', currency: { currency: 'EUR', scale: 2 } },
      { action: 'sort.set', sort_field: 'amount' },
      { action: 'time.set-period', period: { start: '2024-01-01', end: '2024-01-31' } },
      { action: 'view.back-to', depth: 0 },
    ] as const

    for (const request of requests) {
      await controller.apply(request)
      expect(client.transition).toHaveBeenLastCalledWith(
        expect.objectContaining(request),
        expect.any(AbortSignal),
      )
    }
  })

  it('uses verified owned history for Esc and replaces with the server parent otherwise', async () => {
    const go = vi.spyOn(history, 'go').mockImplementation(() => undefined)
    const controller = controllerFor(
      clientWith({
        transition: async (body) =>
          projection(body.action === 'view.back' ? 'v=1' : 'v=1&group=category', 0, 2),
      }),
    )
    await controller.hydrate()
    const top = history.state as MoneyflowHistoryState
    await controller.apply({ action: 'view.cycle-grouping' })
    await traverse(controller, { action: 'view.back' }, top)
    expect(go).toHaveBeenCalledWith(-1)

    go.mockClear()
    history.replaceState({ owner: 'foreign' }, '', '/moneyflow/?v=1&group=category')
    await controller.apply({ action: 'view.back' })
    expect(go).not.toHaveBeenCalled()
    expect(window.location.search).toBe('?v=1')
  })

  it('keeps one history entry per view when Esc returns after a selection change', async () => {
    const go = vi.spyOn(history, 'go').mockImplementation(() => undefined)
    const selected = 'mfsel1.selected' as SelectionValue
    const cleared = 'mfsel1.cleared' as SelectionValue
    const client = clientWith({
      view: vi.fn(async (body) => projection('v=1', body.window.offset, 3, '0', cleared)),
      transition: vi.fn(async (body) => {
        if (body.action === 'selection.toggle') return projection('v=1', 0, 3, '0', selected)
        if (body.action === 'view.drill') {
          return projection('v=1&drill=x', 0, 3, '0', body.selection as SelectionValue)
        }
        return projection('v=1', 0, 3, '0', cleared)
      }),
    })
    const controller = controllerFor(client)
    await controller.hydrate()
    await controller.apply({
      action: 'selection.toggle',
      target: { kind: 'transaction', identity: 'row-0' },
    })
    const topLevel = history.state as MoneyflowHistoryState
    await controller.apply({ action: 'view.drill', target: { kind: 'aggregate', identity: 'x' } })
    vi.mocked(client.view).mockClear()

    await traverse(controller, { action: 'view.back' }, topLevel)
    expect(go).toHaveBeenCalledWith(-1)

    expect(client.view).not.toHaveBeenCalled()
    expect(controller.projection?.selection).toBe(cleared)
    expect((history.state as MoneyflowHistoryState).selection).toBe(cleared)
    expect((history.state as MoneyflowHistoryState).sequence).toBe(topLevel.sequence)
  })

  it('replaces in place when Esc returns the query the current entry already holds', async () => {
    const go = vi.spyOn(history, 'go').mockImplementation(() => undefined)
    const pushes = vi.spyOn(history, 'pushState')
    const controller = controllerFor(clientWith())
    await controller.hydrate()
    go.mockClear()
    pushes.mockClear()

    await controller.apply({ action: 'view.back' })

    expect(go).not.toHaveBeenCalled()
    expect(pushes).not.toHaveBeenCalled()
    expect((history.state as MoneyflowHistoryState).sequence).toBe(0)
  })

  it('returns to a breadcrumb level through owned history', async () => {
    const go = vi.spyOn(history, 'go').mockImplementation(() => undefined)
    const controller = controllerFor(
      clientWith({
        transition: async (body) =>
          projection(body.action === 'view.back-to' ? 'v=1' : `v=1&drill=${body.action}`, 0, 2),
      }),
    )
    await controller.hydrate()
    const top = history.state as MoneyflowHistoryState
    await controller.apply({ action: 'view.drill' })
    await controller.apply({ action: 'view.drill-again' })
    go.mockClear()

    await traverse(controller, { action: 'view.back-to', depth: 0 }, top)

    expect(go).toHaveBeenCalledWith(-2)
    expect(controller.projection?.canonical_query).toBe('v=1')
    expect((history.state as MoneyflowHistoryState).sequence).toBe(top.sequence)
  })

  it('runs a key pressed during an Esc return from the returned view', async () => {
    const go = vi.spyOn(history, 'go').mockImplementation(() => undefined)
    const stale = 'mfsel1.stale' as SelectionValue
    const cleared = 'mfsel1.cleared' as SelectionValue
    const parents: Record<string, string> = { 'v=1&d=2': 'v=1&d=1', 'v=1&d=1': 'v=1' }
    const client = clientWith({
      view: vi.fn(async (body) => projection('v=1', body.window.offset, 3, '0', cleared)),
      transition: vi.fn(async (body) =>
        body.action === 'view.back'
          ? projection(parents[body.query] ?? body.query, 0, 3, '0', cleared)
          : projection(`v=1&d=${body.query === 'v=1' ? 1 : 2}`, 0, 3, '0', stale),
      ),
    })
    const controller = controllerFor(client)
    await controller.hydrate()
    const states = [history.state as MoneyflowHistoryState]
    await controller.apply({ action: 'view.drill' })
    states.push(history.state as MoneyflowHistoryState)
    await controller.apply({ action: 'view.drill' })
    states.push(history.state as MoneyflowHistoryState)
    go.mockClear()
    vi.mocked(client.transition).mockClear()

    const first = controller.apply({ action: 'view.back' })
    await vi.waitFor(() => expect(go).toHaveBeenCalledTimes(1), { interval: 1 })
    const second = controller.apply({ action: 'view.back' })
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(client.transition).toHaveBeenCalledTimes(1)

    await controller.restore(new PopStateEvent('popstate', { state: states[1] }))
    await first
    await vi.waitFor(() => expect(go).toHaveBeenCalledTimes(2), { interval: 1 })
    expect(client.transition).toHaveBeenLastCalledWith(
      expect.objectContaining({ query: 'v=1&d=1', selection: cleared }),
      expect.any(AbortSignal),
    )
    await controller.restore(new PopStateEvent('popstate', { state: states[0] }))
    await second

    expect(client.view).toHaveBeenCalledTimes(1)
    expect(controller.projection?.canonical_query).toBe('v=1')
    expect(controller.projection?.selection).toBe(cleared)
    expect((history.state as MoneyflowHistoryState).sequence).toBe(0)
  })

  it('falls back to replacing the entry when the history traversal never arrives', async () => {
    vi.spyOn(history, 'go').mockImplementation(() => undefined)
    const controller = controllerFor(
      clientWith({
        transition: async (body) =>
          projection(body.action === 'view.back' ? 'v=1' : 'v=1&group=category', 0, 2),
      }),
    )
    await controller.hydrate()
    await controller.apply({ action: 'view.cycle-grouping' })

    await expect(controller.apply({ action: 'view.back' })).resolves.toBe(true)

    expect(controller.projection?.canonical_query).toBe('v=1')
    expect(window.location.search).toBe('?v=1')
    expect(controller.loading).toBe(false)
  })

  it('fetches the parent cursor window when the returned window differs', async () => {
    vi.spyOn(history, 'go').mockImplementation(() => undefined)
    const cleared = 'mfsel1.cleared' as SelectionValue
    const client = clientWith({
      view: vi.fn(async (body) => projection('v=1', body.window.offset, 450, '0', cleared)),
      transition: vi.fn(async (body) =>
        body.action === 'view.back'
          ? projection('v=1', body.window.offset, 450, '0', cleared)
          : projection('v=1&drill=x', 0, 450),
      ),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    await controller.moveCursorTo(250)
    const parent = history.state as MoneyflowHistoryState
    expect(parent.cursorIndex).toBe(250)
    await controller.apply({ action: 'view.drill' })
    await controller.moveHome()
    vi.mocked(client.view).mockClear()

    await traverse(controller, { action: 'view.back' }, parent)

    expect(client.view).toHaveBeenCalledTimes(1)
    expect(client.view).toHaveBeenCalledWith(
      expect.objectContaining({ selection: cleared, window: { offset: 200, limit: 200 } }),
    )
    expect(controller.projection?.window.offset).toBe(200)
    expect(controller.cursorIndex).toBe(250)
    expect(controller.loading).toBe(false)
  })

  it('holds keys behind the first-load currency transition instead of aborting it', async () => {
    let release!: (value: ViewProjection) => void
    const pending = new Promise<ViewProjection>((resolve) => (release = resolve))
    const client = clientWith({
      view: vi.fn(async (body) => ({
        ...projection('v=1', body.window.offset, 3),
        currencies: [
          { currency: 'EUR', scale: 2, count: 3, first: '2024-01-01', latest: '2024-12-31' },
        ],
      })),
      transition: vi
        .fn()
        .mockImplementationOnce(async () => pending)
        .mockImplementation(async (body) => projection(`${body.query}&group=category`, 0, 3)),
    })
    const controller = controllerFor(client)

    const hydrating = controller.hydrate()
    await vi.waitFor(() => expect(client.transition).toHaveBeenCalledTimes(1), { interval: 1 })
    const key = controller.apply({ action: 'view.cycle-grouping' })
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(client.transition).toHaveBeenCalledTimes(1)
    expect(vi.mocked(client.transition).mock.calls[0]?.[1]?.aborted).toBe(false)

    release(projection('cur=EUR%3A2&v=1', 0, 3))
    await Promise.all([hydrating, key])

    expect(client.transition).toHaveBeenCalledTimes(2)
    expect(client.transition).toHaveBeenLastCalledWith(
      expect.objectContaining({ query: 'cur=EUR%3A2&v=1', action: 'view.cycle-grouping' }),
      expect.any(AbortSignal),
    )
    expect(controller.projection?.canonical_query).toBe('cur=EUR%3A2&v=1&group=category')
  })

  it('restores owned Back/Forward state including cursor and selection', async () => {
    const client = clientWith()
    const controller = controllerFor(client)
    await controller.hydrate()
    const state = {
      ...(history.state as MoneyflowHistoryState),
      sequence: 7,
      cursorIdentity: 'row-2',
      cursorIndex: 2,
      selection: 'mfsel1.restored' as ViewProjection['selection'],
    }

    await controller.restore(new PopStateEvent('popstate', { state }))

    expect(controller.cursorIdentity).toBe('row-2')
    expect(controller.cursorIndex).toBe(2)
    expect(client.view).toHaveBeenLastCalledWith(
      expect.objectContaining({ selection: 'mfsel1.restored' }),
      expect.any(AbortSignal),
    )
    expect((history.state as MoneyflowHistoryState).sequence).toBe(state.sequence)
  })

  it('refetches the last valid window after a narrowing transition empties the old offset', async () => {
    history.replaceState(
      {
        owner: 'moneyflow-web-v1',
        instance: 'test-instance',
        sequence: 0,
        query: 'v=1',
        cursorIndex: 400,
        scrollTop: 0,
        selection: 'mfsel1.example' as SelectionValue,
      } satisfies MoneyflowHistoryState,
      '',
      '/moneyflow/?v=1',
    )
    const client = clientWith({
      view: vi.fn(async (body) =>
        projection(body.query, body.window.offset, body.query === 'v=1' ? 450 : 1),
      ),
      transition: vi.fn(async (body) => projection('q=narrow&v=1', body.window.offset, 1)),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    expect(controller.cursorIndex).toBe(400)

    await controller.apply({ action: 'search.apply', search: 'narrow' })

    expect(controller.projection?.window.offset).toBe(0)
    expect(controller.cursorIndex).toBe(0)
    expect(client.view).toHaveBeenLastCalledWith(
      expect.objectContaining({ query: 'q=narrow&v=1', window: { offset: 0, limit: 200 } }),
      expect.any(AbortSignal),
    )
  })

  it('clears an opening search snapshot when browser history restores another view', async () => {
    const client = clientWith({
      view: vi.fn(async (body) => projection(body.query, body.window.offset, 3)),
      transition: vi.fn(async (body) => projection(body.query, 0, 3)),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    controller.beginSearch()
    const restored = {
      ...(history.state as MoneyflowHistoryState),
      query: 'group=category&v=1',
      sequence: 4,
    }
    await controller.restore(new PopStateEvent('popstate', { state: restored }))
    await controller.previewSearch('coffee')
    expect(client.transition).toHaveBeenLastCalledWith(
      expect.objectContaining({ query: 'group=category&v=1' }),
      expect.any(AbortSignal),
    )
  })

  it('moves synchronously in a loaded window and fetches an uncached boundary window', async () => {
    const client = clientWith({
      view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    const calls = vi.mocked(client.view).mock.calls.length
    const loaded = controller.projection

    await controller.moveCursor(1)
    expect(controller.cursorIndex).toBe(1)
    expect(client.view).toHaveBeenCalledTimes(calls)
    expect(controller.projection).toBe(loaded)

    for (let index = 1; index < 200; index += 1) await controller.moveCursor(1)
    expect(controller.cursorIndex).toBe(200)
    expect(client.view).toHaveBeenCalledTimes(calls + 1)
    expect(controller.projection).not.toBe(loaded)

    await controller.moveHome()
    expect(controller.cursorIndex).toBe(0)
    expect(controller.projection).toBe(loaded)
  })

  it('aborts superseded requests and rejects stale responses', async () => {
    let releaseFirst!: (value: ViewProjection) => void
    const first = new Promise<ViewProjection>((resolve) => (releaseFirst = resolve))
    const client = clientWith({
      view: vi
        .fn()
        .mockImplementationOnce(async () => first)
        .mockResolvedValueOnce(projection('v=1&search=new', 0, 1)),
    })
    const controller = controllerFor(client, false)

    const stale = controller.hydrate()
    history.replaceState(null, '', '/moneyflow/?v=1&search=new')
    const current = controller.hydrate()
    releaseFirst(projection('v=1&search=old', 0, 1))
    await Promise.all([stale, current])

    expect(controller.projection?.canonical_query).toBe('v=1&search=new')
    expect(vi.mocked(client.view).mock.calls[0]?.[1]?.aborted).toBe(true)
  })

  it('cannot install an older foreground response after accepting a mutation', async () => {
    let release!: (value: ViewProjection) => void
    const delayed = new Promise<ViewProjection>((resolve) => (release = resolve))
    const client = clientWith({
      view: vi
        .fn()
        .mockResolvedValueOnce(projection('v=1', 0, 3, '1'))
        .mockImplementationOnce(async () => await delayed),
      mutations: {
        request: vi.fn(async () => {
          const next = projection('v=1', 0, 3, '2')
          return Response.json({
            version: '1',
            revision: '2',
            canonical_query: 'v=1',
            projection: next,
            pending: next.pending,
            selection: { kind: 'preserved', value: next.selection },
          })
        }),
      },
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()

    const stale = controller.hydrate()
    await expect(controller.editing.undo()).resolves.toBe(true)
    release(projection('v=1', 0, 3, '1'))
    await stale

    expect(controller.projection?.revision).toBe('2')
    expect(vi.mocked(client.view).mock.calls[1]?.[1]?.aborted).toBe(true)
  })

  it('retains the last good projection on bounded errors and supports retry', async () => {
    const problem = new MoneyflowProblem({
      type: 'about:blank',
      title: 'Conflict',
      status: 409,
      detail: 'oversized private detail',
      code: 'selection_too_large',
    })
    const client = clientWith({
      transition: vi
        .fn()
        .mockRejectedValueOnce(problem)
        .mockResolvedValueOnce(projection('v=1', 0, 3)),
    })
    const controller = controllerFor(client)
    await controller.hydrate()
    const prior = controller.projection

    await controller.apply({ action: 'selection.toggle-all' })
    expect(controller.projection).toBe(prior)
    expect(controller.announcement).not.toContain('private')
    await controller.retry()
    expect(client.transition).toHaveBeenCalledTimes(2)
  })

  it('replaces search previews, commits one entry, and restores the opening snapshot', async () => {
    const pushes = vi.spyOn(history, 'pushState')
    const replacements = vi.spyOn(history, 'replaceState')
    const client = clientWith({
      transition: vi.fn(async (body) =>
        projection(`v=1&q=${encodeURIComponent(body.search ?? '')}`, 0, 1),
      ),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    const snapshot = controller.beginSearch()
    pushes.mockClear()
    replacements.mockClear()

    expect(await controller.previewSearch('coffee')).toBe(true)
    expect(await controller.previewSearch('coffee shop')).toBe(true)
    expect(pushes).not.toHaveBeenCalled()
    expect(replacements).toHaveBeenCalledTimes(2)
    expect(vi.mocked(client.transition).mock.calls[1]?.[0].query).toBe('v=1')

    controller.commitSearch(snapshot)
    expect(pushes).toHaveBeenCalledTimes(1)
    expect(window.location.search).toBe('?v=1&q=coffee%20shop')

    const cancelSnapshot = controller.beginSearch()
    await controller.previewSearch('temporary')
    controller.restoreSearch(cancelSnapshot)
    expect(controller.projection?.canonical_query).toBe('v=1&q=coffee%20shop')
  })

  it('aborts an in-flight search preview when cancel restores its snapshot', async () => {
    let release!: (value: ViewProjection) => void
    const delayed = new Promise<ViewProjection>((resolve) => (release = resolve))
    const controller = controllerFor(clientWith({ transition: vi.fn(async () => delayed) }), false)
    await controller.hydrate()
    const snapshot = controller.beginSearch()

    const preview = controller.previewSearch('temporary')
    expect(controller.loading).toBe(true)
    controller.restoreSearch(snapshot)
    expect(controller.loading).toBe(false)
    release(projection('v=1&q=temporary', 0, 1))
    await preview

    expect(controller.projection?.canonical_query).toBe('v=1')
  })

  it('exposes a safe invalid-view state for malformed direct URLs', async () => {
    const client = clientWith({
      view: async () => {
        throw new MoneyflowProblem({
          type: 'about:blank',
          title: 'Bad Request',
          status: 400,
          detail: 'raw URL data',
          code: 'invalid_view_state',
        })
      },
    })
    const controller = controllerFor(client)

    await controller.hydrate()

    expect(controller.problem).toEqual({ kind: 'invalid-view', code: 'invalid_view_state' })
    expect(controller.announcement).not.toContain('raw URL')
  })

  it('deduplicates passive revision checks and preserves stable cursor without history writes', async () => {
    let release!: (value: ViewProjection) => void
    const passive = new Promise<ViewProjection>((resolve) => (release = resolve))
    const client = clientWith({
      view: vi
        .fn()
        .mockResolvedValueOnce(projection('v=1', 0, 3))
        .mockImplementationOnce(async () => await passive),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    await controller.moveCursorTo(1)
    const pushes = vi.spyOn(history, 'pushState')
    const replacements = vi.spyOn(history, 'replaceState')
    pushes.mockClear()
    replacements.mockClear()

    const first = controller.recheck()
    const duplicate = controller.recheck()
    release(projection('v=1', 0, 3, '2'))
    await Promise.all([first, duplicate])

    expect(client.view).toHaveBeenCalledTimes(2)
    expect(controller.projection?.revision).toBe('2')
    expect(controller.cursorIdentity).toBe('row-1')
    expect(pushes).not.toHaveBeenCalled()
    expect(replacements).not.toHaveBeenCalled()
    expect(controller.editing.state.revision).toBe(2n)
  })

  it('replaces the owned history selection after provider refresh', async () => {
    const initial = projection('v=1', 0, 3, '1')
    initial.capabilities = [
      {
        id: 'provider.refresh',
        key_display: 'r',
        description: 'Refresh provider data',
        category: 'System',
        available: true,
      },
    ]
    const next = projection('v=1', 0, 3, '2')
    next.selection = 'mfsel1.refreshed' as SelectionValue
    const client = clientWith({
      view: vi.fn(async () => initial),
      mutations: {
        request: vi.fn(async () => Response.json(providerRefreshResponse(next))),
      },
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    const replacements = vi.spyOn(history, 'replaceState')
    replacements.mockClear()

    await expect(controller.provider.refresh()).resolves.toBe(true)

    expect(replacements).toHaveBeenCalledTimes(1)
    expect(history.state.selection).toBe(next.selection)
    expect(history.state.cursorIndex).toBe(controller.cursorIndex)
  })

  it('synchronizes editing state after an authoritative write reconciliation', async () => {
    const next = projection('v=1', 0, 3, '2')
    const client = clientWith({
      mutations: {
        request: vi.fn(async () =>
          Response.json({
            status: {
              version: '1',
              revision: '2',
              generation: '1',
              total: 0,
              completed: 0,
              failed: 0,
              remaining: 0,
              overrides: 0,
              actions: [],
            },
            projection: next,
            selection: { kind: 'cleared', value: next.selection },
          }),
        ),
      },
    })
    const controller = controllerFor(client)
    await controller.hydrate()
    controller.providerWrite.install({
      version: '1',
      revision: '1',
      generation: '1',
      batch_version: '4',
      phase: 'paused',
      total: 1,
      completed: 0,
      failed: 0,
      remaining: 1,
      overrides: 0,
      actions: ['reconcile'],
    })

    await expect(controller.providerWrite.reconcile()).resolves.toBe(true)
    expect(controller.editing.state.revision).toBe(2n)
  })
})

function controllerFor(client: MoneyflowClient, prefetch = true) {
  return createViewController({
    basePath: '/moneyflow/',
    client,
    history,
    location: window.location,
    instance: 'test-instance',
    prefetch,
    recheckDebounceMillis: 0,
    returnTimeoutMillis: 200,
  })
}

async function traverse(
  controller: ReturnType<typeof controllerFor>,
  action: Parameters<ReturnType<typeof controllerFor>['apply']>[0],
  target: MoneyflowHistoryState,
): Promise<void> {
  const go = vi.mocked(history.go)
  const before = go.mock.calls.length
  const done = controller.apply(action)
  await vi.waitFor(() => expect(go.mock.calls.length).toBeGreaterThan(before), { interval: 1 })
  await controller.restore(new PopStateEvent('popstate', { state: target }))
  await done
}

function clientWith(overrides: Partial<MoneyflowClient> = {}): MoneyflowClient {
  return {
    mutations: { request: vi.fn(async () => new Response(null, { status: 204 })) },
    providerStatus: vi.fn(async () => {
      throw new Error('provider status not stubbed')
    }),
    providerWriteStatus: vi.fn(async () => ({
      version: '1',
      revision: '0',
      generation: '0',
      total: 0,
      completed: 0,
      failed: 0,
      remaining: 0,
      overrides: 0,
      actions: [],
    })),
    view: vi.fn(async (body) => projection('v=1', body.window.offset, 3)),
    transition: vi.fn(async (body) => projection(body.query, body.window.offset, 3)),
    projectDuplicates: vi.fn(async () => {
      throw new Error('duplicate projection not stubbed')
    }),
    previewExport: vi.fn(async () => {
      throw new Error('export preview not stubbed')
    }),
    downloadExport: vi.fn(async () => {
      throw new Error('export download not stubbed')
    }),
    ...overrides,
  }
}

function projection(
  query: string,
  offset: number,
  total: number,
  revision = '0',
  selection = 'mfsel1.example',
): ViewProjection {
  const count = Math.max(0, Math.min(200, total - offset))
  return {
    api_schema_version: '1',
    projection_schema_version: '1',
    profile_kind: 'local',
    amazon_match_column: false,
    revision,
    pending: { active_operations: 0, inactive_operations: 0, affected_transactions: 0 },
    canonical_query: query,
    view: {
      mode: 'detail',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'date',
      sort_direction: 'desc',
    },
    selection: selection as ViewProjection['selection'],
    selection_count: 0,
    breadcrumbs: [],
    breadcrumb_text: 'All transactions',
    filters: { show_hidden: false, show_transfers: false },
    capabilities: [],
    currencies: [],
    total_rows: total,
    window: { offset, limit: 200, count },
    detail_rows: Array.from({ length: count }, (_, index) => ({
      index: offset + index,
      identity: `row-${offset + index}`,
      date: '2024-01-01',
      account: 'Account Name',
      merchant: 'Example Merchant',
      category: 'Example Category',
      group: 'Example Group',
      amount: { minor: '-100', currency: 'USD', scale: 2, decimal: '-1.00', display: '-$1.00' },
      flags: { selected: false, hidden: false, pending: false },
    })),
    statistics: [],
    chart: {},
  }
}

function providerRefreshResponse(next: ViewProjection) {
  return {
    version: '1',
    revision: next.revision,
    generation: '2',
    status: {
      version: '1',
      revision: next.revision,
      generation: '2',
      progress: { fetched: 0, total: 0 },
      summary: {
        imported_accounts: 0,
        imported_merchants: 0,
        imported_groups: 0,
        imported_categories: 0,
        imported_transactions: 3,
        removed_transactions: 0,
        removed_operations: 0,
        removed_targets: 0,
        retained_operations: 0,
        rebased_hide_targets: 0,
        discarded_redo_operations: 0,
      },
      capability: {
        id: 'provider.refresh',
        key_display: 'r',
        description: 'Refresh provider data',
        category: 'System',
        available: true,
      },
    },
    projection: next,
    selection: { kind: 'preserved', value: next.selection },
  }
}
