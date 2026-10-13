import { dismissFlash, getFlashes } from '@kenn-io/kit-ui'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  MoneyflowProblem,
  type MoneyflowClient,
  type ViewBody,
  type ViewProjection,
} from '../api/client'
import { createViewController, type ViewController } from './view-controller.svelte'
import type { SelectionValue } from '../api/client'
import type { MoneyflowHistoryState } from './history'
import { testAggregateRow } from '../../test/projection'

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

    it('sets the currency once an empty profile receives its first transactions', async () => {
      let populated = false
      const client = clientWith({
        view: vi.fn(async (body) => ({
          ...projection('v=1', body.window.offset, populated ? 3 : 0, populated ? '1' : '0'),
          currencies: populated ? currencies([['USD', 3]]) : [],
        })),
        transition: vi.fn(async (body) => ({
          ...projection('cur=USD%3A2&v=1', body.window.offset, 3, '1'),
          view: { ...projection('v=1', 0, 3).view, currency: { currency: 'USD', scale: 2 } },
          currencies: currencies([['USD', 3]]),
        })),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      expect(client.transition).not.toHaveBeenCalled()

      populated = true
      await controller.recheck()

      await vi.waitFor(() =>
        expect(controller.projection?.view.currency).toEqual({ currency: 'USD', scale: 2 }),
      )
      expect(client.transition).toHaveBeenCalledWith(
        expect.objectContaining({
          action: 'view.set-currency',
          currency: { currency: 'USD', scale: 2 },
        }),
        expect.any(AbortSignal),
      )
      expect(window.location.search).toBe('?cur=USD%3A2&v=1')
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

  it('runs each transition from the view the one before it returned', async () => {
    const releases: Array<() => void> = []
    const client = clientWith({
      transition: vi.fn(
        (body) =>
          new Promise<ViewProjection>((resolve) =>
            releases.push(() => resolve(projection(`${body.query}&p`, 0, 3))),
          ),
      ),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()

    const first = controller.apply({ action: 'time.previous-period' })
    const second = controller.apply({ action: 'time.previous-period' })
    await vi.waitFor(() => expect(releases).toHaveLength(1), { interval: 1 })
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(client.transition).toHaveBeenCalledTimes(1)
    expect(vi.mocked(client.transition).mock.calls[0]?.[1]?.aborted).toBe(false)
    releases[0]?.()
    await vi.waitFor(() => expect(releases).toHaveLength(2), { interval: 1 })
    releases[1]?.()
    await Promise.all([first, second])

    expect(client.transition).toHaveBeenLastCalledWith(
      expect.objectContaining({ query: 'v=1&p', action: 'time.previous-period' }),
      expect.any(AbortSignal),
    )
    expect(controller.projection?.canonical_query).toBe('v=1&p&p')
  })

  it('sends a filter change with the period of the view it starts from', async () => {
    let release!: () => void
    const september = { from: '2026-09-01', to: '2026-09-30' }
    const client = clientWith({
      transition: vi.fn(async (body) => {
        const next = projection(`${body.query}&${body.action}`, 0, 3)
        if (body.action !== 'time.set-period') return next
        await new Promise<void>((resolve) => (release = resolve))
        return { ...next, filters: { ...next.filters, date_range: september } }
      }),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()

    await controller.apply({
      action: 'filters.apply',
      filters: { show_hidden: true, show_transfers: false },
    })
    const period = controller.apply({
      action: 'time.set-period',
      period: { start: september.from, end: september.to },
    })
    const filters = controller.apply({
      action: 'filters.apply',
      filters: { show_hidden: true, show_transfers: true },
    })
    await vi.waitFor(() => expect(release).toBeDefined(), { interval: 1 })
    release()
    await Promise.all([period, filters])

    const sent = vi.mocked(client.transition).mock.calls.map(([body]) => body.filters)
    expect(sent).toEqual([
      { show_hidden: true, show_transfers: false },
      undefined,
      {
        date_range: { start: '2026-09-01', end: '2026-09-30' },
        show_hidden: true,
        show_transfers: true,
      },
    ])
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
    const client = clientWith({
      transition: async (body) =>
        projection(body.action === 'view.back-to' ? 'v=1' : `v=1&drill=${body.action}`, 0, 2),
    })
    const controller = controllerFor(client)
    await controller.hydrate()
    const top = history.state as MoneyflowHistoryState
    await controller.apply({ action: 'view.drill' })
    await controller.apply({ action: 'view.drill-again' })
    go.mockClear()
    vi.mocked(client.view).mockClear()

    await traverse(controller, { action: 'view.back-to', depth: 0 }, top)

    expect(go).toHaveBeenCalledWith(-2)
    expect(client.view).not.toHaveBeenCalled()
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

  describe('a history traversal that does not arrive in time', () => {
    async function timedOut() {
      vi.spyOn(history, 'go').mockImplementation(() => undefined)
      const client = clientWith({
        transition: async (body) =>
          projection(body.action === 'view.back' ? 'v=1' : 'v=1&group=category', 0, 2),
      })
      const controller = controllerFor(client)
      await controller.hydrate()
      const parent = history.state as MoneyflowHistoryState
      await controller.apply({ action: 'view.cycle-grouping' })
      const drill = history.state as MoneyflowHistoryState
      vi.mocked(client.view).mockClear()
      await expect(controller.apply({ action: 'view.back' })).resolves.toBe(true)
      return { client, controller, parent, drill }
    }

    it('shows the parent without writing it over the drill entry', async () => {
      const { controller, drill } = await timedOut()

      expect(controller.projection?.canonical_query).toBe('v=1')
      expect(controller.loading).toBe(false)
      expect(history.state).toEqual(drill)
      expect(window.location.search).toBe('?v=1&group=category')
    })

    it('installs a late matching popstate without a request', async () => {
      const { client, controller, parent } = await timedOut()

      await controller.restore(new PopStateEvent('popstate', { state: parent }))

      expect(client.view).not.toHaveBeenCalled()
      expect(controller.projection?.canonical_query).toBe('v=1')
      expect((history.state as MoneyflowHistoryState).sequence).toBe(parent.sequence)
      expect(window.location.search).toBe('?v=1')
    })

    it('fetches the parent on a late popstate after an edit replaced the view', async () => {
      const { client, controller, parent } = await timedOut()
      vi.mocked(client.view).mockImplementation(async (body) =>
        projection(body.query, body.window.offset, 2, '1'),
      )
      await controller.recheck()
      expect(controller.projection?.revision).toBe('1')
      vi.mocked(client.view).mockClear()

      await controller.restore(new PopStateEvent('popstate', { state: parent }))

      expect(client.view).toHaveBeenCalledTimes(1)
      expect(controller.projection?.canonical_query).toBe('v=1')
      expect(controller.projection?.revision).toBe('1')
    })

    it('forgets the expected target once another popstate arrives', async () => {
      const { client, controller, parent, drill } = await timedOut()

      await controller.restore(new PopStateEvent('popstate', { state: drill }))
      expect(client.view).toHaveBeenCalledTimes(1)
      await controller.restore(new PopStateEvent('popstate', { state: parent }))

      expect(client.view).toHaveBeenCalledTimes(2)
    })
  })

  describe('an edit that lands between Esc and its history traversal', () => {
    const cleared = 'mfsel1.cleared' as SelectionValue

    async function editDuringReturn() {
      const go = vi.spyOn(history, 'go').mockImplementation(() => undefined)
      const client = clientWith({
        transition: vi.fn(async (body) =>
          body.action === 'view.back'
            ? projection('v=1', 0, 3, '0', cleared)
            : projection('v=1&drill=x', 0, 3),
        ),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      const parent = history.state as MoneyflowHistoryState
      await controller.apply({ action: 'view.drill' })
      const before = go.mock.calls.length
      const back = controller.apply({ action: 'view.back' })
      await vi.waitFor(() => expect(go.mock.calls.length).toBeGreaterThan(before), { interval: 1 })
      vi.mocked(client.view).mockImplementation(async (body) =>
        projection(body.query, body.window.offset, 3, '1'),
      )
      await controller.recheck()
      expect(controller.projection?.revision).toBe('1')
      vi.mocked(client.view).mockClear()
      return { client, controller, parent, back }
    }

    it('fetches the parent at the edited revision when the popstate arrives', async () => {
      const { client, controller, parent, back } = await editDuringReturn()

      await controller.restore(new PopStateEvent('popstate', { state: parent }))
      await back

      expect(client.view).toHaveBeenCalledWith(
        expect.objectContaining({ query: 'v=1' }),
        expect.any(AbortSignal),
      )
      expect(controller.projection?.canonical_query).toBe('v=1')
      expect(controller.projection?.revision).toBe('1')
    })

    it('fetches the parent with the selection Esc left, not the one history saved', async () => {
      const { client, controller, parent, back } = await editDuringReturn()
      expect(parent.selection).not.toBe(cleared)

      await controller.restore(new PopStateEvent('popstate', { state: parent }))
      await back

      expect(client.view).toHaveBeenCalledWith(
        expect.objectContaining({ query: 'v=1', selection: cleared }),
        expect.any(AbortSignal),
      )
    })

    it('holds a key pressed during the parent fetch until the parent loads', async () => {
      const { client, controller, parent, back } = await editDuringReturn()
      let release!: (value: ViewProjection) => void
      vi.mocked(client.view).mockImplementationOnce(
        () => new Promise<ViewProjection>((resolve) => (release = resolve)),
      )
      vi.mocked(client.transition).mockClear()

      const restoring = controller.restore(new PopStateEvent('popstate', { state: parent }))
      const key = controller.apply({ action: 'view.drill' })
      await new Promise((resolve) => setTimeout(resolve, 10))

      expect(client.transition).not.toHaveBeenCalled()
      expect(vi.mocked(client.view).mock.calls.at(-1)?.[1]?.aborted).toBe(false)
      release(projection('v=1', 0, 3, '1'))
      await Promise.all([restoring, back, key])
      expect(client.transition).toHaveBeenCalledWith(
        expect.objectContaining({ query: 'v=1', action: 'view.drill' }),
        expect.any(AbortSignal),
      )
    })

    it('keeps the edited view when the traversal times out', async () => {
      const { controller, back } = await editDuringReturn()

      await back

      expect(controller.projection?.revision).toBe('1')
    })
  })

  it('runs later transitions after installing a returned view fails', async () => {
    const go = vi.spyOn(history, 'go').mockImplementation(() => undefined)
    const client = clientWith({
      transition: vi.fn(async (body) =>
        projection(body.action === 'view.back' ? 'v=1' : 'v=1&drill=x', 0, 3),
      ),
    })
    const controller = controllerFor(client)
    await controller.hydrate()
    const parent = history.state as MoneyflowHistoryState
    await controller.apply({ action: 'view.drill' })
    const before = go.mock.calls.length
    const back = controller.apply({ action: 'view.back' })
    await vi.waitFor(() => expect(go.mock.calls.length).toBeGreaterThan(before), { interval: 1 })
    vi.spyOn(history, 'replaceState').mockImplementationOnce(() => {
      throw new Error('history is unavailable')
    })

    await expect(
      controller.restore(new PopStateEvent('popstate', { state: parent })),
    ).rejects.toThrow('history is unavailable')
    await back

    await expect(controller.apply({ action: 'view.drill' })).resolves.toBe(true)
    expect(controller.projection?.canonical_query).toBe('v=1&drill=x')
  })

  it('installs the returned view at once, then fetches the parent cursor window', async () => {
    vi.spyOn(history, 'go').mockImplementation(() => undefined)
    const cleared = 'mfsel1.cleared' as SelectionValue
    let release!: (value: ViewProjection) => void
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
    vi.mocked(client.view).mockImplementationOnce(
      () => new Promise<ViewProjection>((resolve) => (release = resolve)),
    )

    const go = vi.mocked(history.go)
    const before = go.mock.calls.length
    const done = controller.apply({ action: 'view.back' })
    await vi.waitFor(() => expect(go.mock.calls.length).toBeGreaterThan(before), { interval: 1 })
    const restoring = controller.restore(new PopStateEvent('popstate', { state: parent }))
    await vi.waitFor(() => expect(client.view).toHaveBeenCalledTimes(1), { interval: 1 })

    expect(controller.projection?.canonical_query).toBe('v=1')
    expect(controller.projection?.window.offset).toBe(0)
    expect(controller.cursorIndex).toBe(250)
    expect((history.state as MoneyflowHistoryState).query).toBe('v=1')
    expect((history.state as MoneyflowHistoryState).sequence).toBe(parent.sequence)
    expect(client.view).toHaveBeenCalledTimes(1)
    expect(client.view).toHaveBeenCalledWith(
      expect.objectContaining({ selection: cleared, window: { offset: 200, limit: 200 } }),
      expect.any(AbortSignal),
    )

    release(projection('v=1', 200, 450, '0', cleared))
    await Promise.all([restoring, done])
    expect(controller.projection?.window.offset).toBe(200)
    expect(controller.cursorIndex).toBe(250)
    expect(controller.loading).toBe(false)
  })

  it('keeps the parent window when an edit shrinks it below the cursor before Esc', async () => {
    vi.spyOn(history, 'go').mockImplementation(() => undefined)
    let revision = '0'
    let parentRows = 201
    const client = clientWith({
      view: vi.fn(async (body) =>
        body.query === 'v=1&drill=x'
          ? projection(body.query, 0, 3, revision)
          : projection('v=1', body.window.offset, parentRows, revision),
      ),
      transition: vi.fn(async (body) =>
        body.action === 'view.back'
          ? projection('v=1', body.window.offset, parentRows, revision)
          : projection('v=1&drill=x', 0, 3, revision),
      ),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    await controller.moveCursorTo(200)
    const parent = history.state as MoneyflowHistoryState
    await controller.apply({ action: 'view.drill' })
    revision = '1'
    parentRows = 200
    await controller.recheck()
    expect(controller.projection?.revision).toBe('1')

    await traverse(controller, { action: 'view.back' }, parent)

    expect(controller.projection?.window).toEqual({ offset: 0, limit: 200, count: 200 })
    expect(controller.cursorIndex).toBe(199)
    expect(controller.cursorIdentity).toBe('row-199')
  })

  it('runs a row key pressed during an Esc return after the parent cursor window loads', async () => {
    vi.spyOn(history, 'go').mockImplementation(() => undefined)
    let release!: (value: ViewProjection) => void
    const client = clientWith({
      view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
      transition: vi.fn(async (body) =>
        body.action === 'view.back'
          ? projection('v=1', body.window.offset, 450)
          : projection('v=1&drill=x', 0, 450),
      ),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    await controller.moveCursorTo(250)
    const parent = history.state as MoneyflowHistoryState
    await controller.apply({ action: 'view.drill' })
    await controller.moveHome()
    vi.mocked(client.view).mockImplementationOnce(
      () => new Promise<ViewProjection>((resolve) => (release = resolve)),
    )
    const go = vi.mocked(history.go)
    const before = go.mock.calls.length
    const back = controller.apply({ action: 'view.back' })
    await vi.waitFor(() => expect(go.mock.calls.length).toBeGreaterThan(before), { interval: 1 })
    const restoring = controller.restore(new PopStateEvent('popstate', { state: parent }))
    let cursorAtKey: string | undefined = 'not run'
    const key = back.then(() => {
      cursorAtKey = controller.cursorIdentity
    })
    await new Promise((resolve) => setTimeout(resolve, 10))

    expect(cursorAtKey).toBe('not run')
    expect(vi.mocked(client.view).mock.calls.at(-1)?.[1]?.aborted).toBe(false)
    release(projection('v=1', 200, 450))
    await Promise.all([restoring, key])
    expect(cursorAtKey).toBe('row-250')
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
    expect(controller.opening).toBe(true)
    const key = controller.apply({ action: 'view.cycle-grouping' })
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(client.transition).toHaveBeenCalledTimes(1)
    expect(vi.mocked(client.transition).mock.calls[0]?.[1]?.aborted).toBe(false)

    release(projection('cur=EUR%3A2&v=1', 0, 3))
    await Promise.all([hydrating, key])
    expect(controller.opening).toBe(false)

    expect(client.transition).toHaveBeenCalledTimes(2)
    expect(client.transition).toHaveBeenLastCalledWith(
      expect.objectContaining({ query: 'cur=EUR%3A2&v=1', action: 'view.cycle-grouping' }),
      expect.any(AbortSignal),
    )
    expect(controller.projection?.canonical_query).toBe('cur=EUR%3A2&v=1&group=category')
  })

  it.each([
    ['rejects', () => Promise.reject(new Error('network down'))],
    ['is aborted', () => Promise.reject(new DOMException('aborted', 'AbortError'))],
  ])('releases held keys when the first-load currency change %s', async (_label, fail) => {
    const client = clientWith({
      view: vi.fn(async (body) => ({
        ...projection('v=1', body.window.offset, 3),
        currencies: [
          { currency: 'EUR', scale: 2, count: 3, first: '2024-01-01', latest: '2024-12-31' },
        ],
      })),
      transition: vi
        .fn()
        .mockImplementationOnce(fail)
        .mockImplementation(async (body) => projection(`${body.query}&group=category`, 0, 3)),
    })
    const controller = controllerFor(client)

    const hydrating = controller.hydrate()
    await vi.waitFor(() => expect(client.transition).toHaveBeenCalledTimes(1), { interval: 1 })
    const key = controller.apply({ action: 'view.cycle-grouping' })
    await hydrating

    expect(controller.opening).toBe(false)
    await expect(key).resolves.toBe(true)
    expect(controller.projection?.canonical_query).toBe('v=1&group=category')
  })

  it('keeps a key behind both a pending return and a hydrate started during it', async () => {
    vi.spyOn(history, 'go').mockImplementation(() => undefined)
    let withCurrency = false
    const client = clientWith({
      view: vi.fn(async (body) => ({
        ...projection('v=1', body.window.offset, 3),
        currencies: withCurrency
          ? [{ currency: 'EUR', scale: 2, count: 3, first: '2024-01-01', latest: '2024-12-31' }]
          : [],
      })),
      transition: vi.fn(async (body) => {
        if (body.action === 'view.set-currency') return projection('cur=EUR%3A2&v=1', 0, 3)
        return projection(body.action === 'view.back' ? 'v=1' : 'v=1&drill=x', 0, 3)
      }),
    })
    const controller = controllerFor(client)
    await controller.hydrate()
    const parent = history.state as MoneyflowHistoryState
    await controller.apply({ action: 'view.drill' })
    vi.mocked(client.transition).mockClear()

    const go = vi.mocked(history.go)
    const before = go.mock.calls.length
    const back = controller.apply({ action: 'view.back' })
    await vi.waitFor(() => expect(go.mock.calls.length).toBeGreaterThan(before), { interval: 1 })
    withCurrency = true
    await controller.hydrate()
    const key = controller.apply({ action: 'view.cycle-grouping' })
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(client.transition).not.toHaveBeenCalledWith(
      expect.objectContaining({ action: 'view.cycle-grouping' }),
      expect.anything(),
    )

    await controller.restore(new PopStateEvent('popstate', { state: parent }))
    await Promise.all([back, key])

    expect(client.transition).toHaveBeenLastCalledWith(
      expect.objectContaining({ action: 'view.cycle-grouping', query: 'v=1' }),
      expect.any(AbortSignal),
    )
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

  it('refetches the last valid window when Back returns to an offset past the parent’s rows', async () => {
    history.replaceState(
      {
        owner: 'moneyflow-web-v1',
        instance: 'test-instance',
        sequence: 0,
        query: 'v=1',
        cursorIndex: 400,
        selection: 'mfsel1.example' as SelectionValue,
      } satisfies MoneyflowHistoryState,
      '',
      '/moneyflow/?v=1',
    )
    const client = clientWith({
      view: vi.fn(async (body) =>
        projection(body.query, body.window.offset, body.query === 'v=1' ? 450 : 1),
      ),
      transition: vi.fn(async (body) => projection('group=category&v=1', body.window.offset, 1)),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    expect(controller.cursorIndex).toBe(400)

    await controller.apply({ action: 'view.back' })

    expect(client.transition).toHaveBeenLastCalledWith(
      expect.objectContaining({ window: { offset: 400, limit: 200 } }),
      expect.any(AbortSignal),
    )
    expect(controller.projection?.window.offset).toBe(0)
    expect(controller.cursorIndex).toBe(0)
    expect(client.view).toHaveBeenLastCalledWith(
      expect.objectContaining({ query: 'group=category&v=1', window: { offset: 0, limit: 200 } }),
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

  describe('history writes for cursor moves', () => {
    afterEach(() => vi.useRealTimers())

    const rows = () =>
      clientWith({ view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)) })

    it('writes the last cursor once after a burst of moves', async () => {
      vi.useFakeTimers()
      const controller = debouncedControllerFor(rows())
      await controller.hydrate()
      const replacements = vi.spyOn(history, 'replaceState')
      replacements.mockClear()

      for (let step = 0; step < 100; step += 1) {
        await controller.moveCursor(1)
        await vi.advanceTimersByTimeAsync(10)
      }
      await vi.advanceTimersByTimeAsync(1000)

      expect(replacements.mock.calls.length).toBeGreaterThan(0)
      expect(replacements.mock.calls.length).toBeLessThanOrEqual(3)
      expect((history.state as MoneyflowHistoryState).cursorIndex).toBe(100)
    })

    it('writes the pending cursor before the entry a transition pushes', async () => {
      vi.useFakeTimers()
      const client = clientWith({
        view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
        transition: vi.fn(async () => projection('v=1&drill=x', 0, 3)),
      })
      const controller = debouncedControllerFor(client)
      await controller.hydrate()
      const parent = history.state as MoneyflowHistoryState
      const writes: Array<[string, MoneyflowHistoryState]> = []
      vi.spyOn(history, 'replaceState').mockImplementation(function (this: History, state, _, url) {
        writes.push(['replace', state as MoneyflowHistoryState])
        History.prototype.replaceState.call(this, state, '', url)
      })
      vi.spyOn(history, 'pushState').mockImplementation(function (this: History, state, _, url) {
        writes.push(['push', state as MoneyflowHistoryState])
        History.prototype.pushState.call(this, state, '', url)
      })

      await controller.moveCursorTo(5)
      await controller.moveCursorTo(7)
      await controller.apply({ action: 'view.drill' })
      await vi.advanceTimersByTimeAsync(1000)

      expect(writes.map(([kind, state]) => [kind, state.query, state.cursorIndex])).toEqual([
        ['replace', 'v=1', 7],
        ['push', 'v=1&drill=x', 0],
      ])
      expect(writes[0]?.[1].sequence).toBe(parent.sequence)
    })

    it('writes the pending cursor before going back through history', async () => {
      vi.useFakeTimers()
      const client = clientWith({
        view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
        transition: vi.fn(async (body) =>
          projection(body.action === 'view.back' ? 'v=1' : 'v=1&drill=x', 0, 450),
        ),
      })
      const controller = debouncedControllerFor(client)
      await controller.hydrate()
      await controller.apply({ action: 'view.drill' })
      await controller.moveCursorTo(4)
      let cursorWhenLeaving: number | undefined
      vi.spyOn(history, 'go').mockImplementation(() => {
        cursorWhenLeaving = (history.state as MoneyflowHistoryState).cursorIndex
      })

      void controller.apply({ action: 'view.back' })
      await vi.waitFor(() => expect(cursorWhenLeaving).toBe(4))
    })

    it('writes the pending cursor at once when asked to flush', async () => {
      vi.useFakeTimers()
      const controller = debouncedControllerFor(rows())
      await controller.hydrate()
      const replacements = vi.spyOn(history, 'replaceState')
      replacements.mockClear()

      await controller.moveCursorTo(9)
      controller.flushHistory()
      expect((history.state as MoneyflowHistoryState).cursorIndex).toBe(9)
      await vi.advanceTimersByTimeAsync(1000)

      expect(replacements).toHaveBeenCalledTimes(1)
    })

    it('does not write a pending cursor onto the entry the browser returns to', async () => {
      vi.useFakeTimers()
      const client = clientWith({
        view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
        transition: vi.fn(async () => projection('v=1&drill=x', 0, 3)),
      })
      const controller = debouncedControllerFor(client)
      await controller.hydrate()
      const parent = history.state as MoneyflowHistoryState
      await controller.apply({ action: 'view.drill' })
      await controller.moveCursorTo(2)
      vi.mocked(client.view).mockImplementation(() => new Promise<ViewProjection>(() => undefined))

      history.replaceState(parent, '', '/moneyflow/?v=1')
      void controller.restore(new PopStateEvent('popstate', { state: parent }))
      await vi.advanceTimersByTimeAsync(1000)

      expect(history.state).toEqual(parent)
    })

    it('brings back a pending cursor when Forward returns to the entry Back left', async () => {
      vi.useFakeTimers()
      history.replaceState(null, '', '/moneyflow/?v=1')
      const client = clientWith({
        view: vi.fn(async (body) => projection(body.query, body.window.offset, 450)),
        transition: vi.fn(async () => projection('v=1&drill=x', 0, 450)),
      })
      const controller = debouncedControllerFor(client)
      await controller.hydrate()
      const parent = history.state as MoneyflowHistoryState
      await controller.apply({ action: 'view.drill' })
      const child = history.state as MoneyflowHistoryState
      await controller.moveCursorTo(4)

      history.replaceState(parent, '', '/moneyflow/?v=1')
      await controller.restore(new PopStateEvent('popstate', { state: parent }))
      history.replaceState(child, '', '/moneyflow/?v=1&drill=x')
      await controller.restore(new PopStateEvent('popstate', { state: child }))
      await vi.advanceTimersByTimeAsync(1000)

      expect(controller.cursorIndex).toBe(4)
      expect((history.state as MoneyflowHistoryState).cursorIndex).toBe(4)
    })

    it('forgets a left cursor once a new entry replaces the one Back left', async () => {
      vi.useFakeTimers()
      history.replaceState(null, '', '/moneyflow/?v=1')
      const client = clientWith({
        view: vi.fn(async (body) => projection(body.query, body.window.offset, 450)),
        transition: vi.fn(async () => projection('v=1&drill=x', 0, 450)),
      })
      const controller = debouncedControllerFor(client)
      await controller.hydrate()
      const parent = history.state as MoneyflowHistoryState
      await controller.apply({ action: 'view.drill' })
      await controller.moveCursorTo(4)
      history.replaceState(parent, '', '/moneyflow/?v=1')
      await controller.restore(new PopStateEvent('popstate', { state: parent }))

      await controller.apply({ action: 'view.drill' })
      const replacement = history.state as MoneyflowHistoryState
      history.replaceState(parent, '', '/moneyflow/?v=1')
      await controller.restore(new PopStateEvent('popstate', { state: parent }))
      history.replaceState(replacement, '', '/moneyflow/?v=1&drill=x')
      await controller.restore(new PopStateEvent('popstate', { state: replacement }))

      expect(controller.cursorIndex).toBe(0)
    })
  })

  describe('showing an anchored row', () => {
    /** A window of a 450-merchant view whose row 420 is `merchant:example`. */
    const merchants = (offset: number): ViewProjection => {
      const window = projection('v=1', offset, 450)
      return {
        ...window,
        view: { ...window.view, mode: 'aggregate', sort_field: 'amount' },
        detail_rows: null,
        aggregate_rows: (window.detail_rows ?? []).map(({ index }) =>
          testAggregateRow({
            index,
            identity: index === 420 ? 'merchant:example' : `merchant:${index}`,
          }),
        ),
      }
    }
    const anchored = (body: ViewBody) =>
      body.anchor === 'merchant:example'
        ? { ...merchants(400), anchor_index: 420 }
        : merchants(body.window.offset)

    it('loads the window holding the row and moves the cursor there without a history entry', async () => {
      const client = clientWith({ view: vi.fn(async (body) => anchored(body)) })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      const pushes = vi.spyOn(history, 'pushState')
      pushes.mockClear()

      await controller.showAnchor('merchant:example')

      expect(vi.mocked(client.view).mock.lastCall?.[0]).toEqual({
        query: 'v=1',
        selection: 'mfsel1.example',
        window: { offset: 0, limit: 200 },
        anchor: 'merchant:example',
      })
      expect(controller.projection?.window.offset).toBe(400)
      expect(controller.cursorIndex).toBe(420)
      expect(controller.cursorIdentity).toBe('merchant:example')
      expect((history.state as MoneyflowHistoryState).cursorIndex).toBe(420)
      expect(pushes).not.toHaveBeenCalled()
    })

    it('leaves the view and cursor alone when the row is missing', async () => {
      const client = clientWith({
        view: vi.fn(async (body) => ({
          ...projection('v=1', body.window.offset, 450),
          ...(body.anchor === undefined
            ? {}
            : { warnings: [{ code: 'anchor_missing', detail: 'The row is not in this view.' }] }),
        })),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      await controller.moveCursorTo(3)
      const loaded = controller.projection

      await controller.showAnchor('merchant:gone')

      expect(client.view).toHaveBeenLastCalledWith(
        expect.objectContaining({ anchor: 'merchant:gone' }),
        expect.any(AbortSignal),
      )
      expect(controller.projection).toBe(loaded)
      expect(controller.cursorIndex).toBe(3)
      expect(controller.announcement).toBe('')
    })

    it('waits for held transition work before asking for the row', async () => {
      const client = clientWith({ view: vi.fn(async (body) => anchored(body)) })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      let release!: () => void
      void controller.hold(new Promise<void>((resolve) => (release = resolve)))

      const showing = controller.showAnchor('merchant:example')
      await new Promise((resolve) => setTimeout(resolve, 0))
      expect(client.view).not.toHaveBeenLastCalledWith(
        expect.objectContaining({ anchor: 'merchant:example' }),
        expect.anything(),
      )

      release()
      await showing
      expect(controller.cursorIndex).toBe(420)
      expect(controller.cursorIdentity).toBe('merchant:example')
    })

    it('waits for a command-bar transition still on its way before asking for the row', async () => {
      let answer!: (next: ViewProjection) => void
      const client = clientWith({
        view: vi.fn(async (body: ViewBody) => ({ ...anchored(body), canonical_query: body.query })),
        transition: vi.fn(() => new Promise<ViewProjection>((resolve) => (answer = resolve))),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()

      const applying = controller.apply({ action: 'view.set-subgrouping', dimension: 'account' })
      await new Promise((resolve) => setTimeout(resolve, 0))
      const showing = controller.showAnchor('merchant:example')
      await new Promise((resolve) => setTimeout(resolve, 0))
      answer({ ...merchants(0), canonical_query: 'v=2' })

      await expect(applying).resolves.toBe(true)
      await showing
      expect(vi.mocked(client.view).mock.lastCall?.[0]).toMatchObject({
        query: 'v=2',
        anchor: 'merchant:example',
      })
      expect(controller.projection?.canonical_query).toBe('v=2')
      expect(controller.cursorIdentity).toBe('merchant:example')
    })

    it('keeps a cursor move made while the row loads', async () => {
      let answer!: (next: ViewProjection) => void
      const client = clientWith({
        view: vi.fn((body: ViewBody) =>
          body.anchor === undefined
            ? Promise.resolve(merchants(body.window.offset))
            : new Promise<ViewProjection>((resolve) => (answer = resolve)),
        ),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      const loaded = controller.projection

      const showing = controller.showAnchor('merchant:example')
      await new Promise((resolve) => setTimeout(resolve, 0))
      await controller.moveCursor(1)
      answer({ ...merchants(400), anchor_index: 420 })
      await showing

      expect(controller.cursorIndex).toBe(1)
      expect(controller.cursorIdentity).toBe('merchant:1')
      expect(controller.projection).toBe(loaded)
    })
  })

  describe('rechecking for newer insights', () => {
    it('refetches the view at once when insights report a newer revision', async () => {
      const client = clientWith({
        view: vi
          .fn()
          .mockResolvedValueOnce(projection('v=1', 0, 3, '4'))
          .mockResolvedValueOnce(projection('v=1', 0, 3, '5')),
      })
      const controller = createViewController({
        basePath: '/moneyflow/',
        client,
        history,
        location: window.location,
        instance: 'test-instance',
        prefetch: false,
        recheckDebounceMillis: 60_000,
      })
      await controller.hydrate()

      await controller.recheckNewer('4')
      expect(client.view).toHaveBeenCalledTimes(1)

      await controller.recheckNewer('5')
      expect(client.view).toHaveBeenCalledTimes(2)
      expect(controller.projection?.revision).toBe('5')
    })

    it('waits for held transition work before rechecking', async () => {
      const client = clientWith({
        view: vi
          .fn()
          .mockResolvedValueOnce(projection('v=1', 0, 3, '4'))
          .mockResolvedValueOnce(projection('v=1', 0, 3, '5')),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      let release!: () => void
      void controller.hold(new Promise<void>((resolve) => (release = resolve)))

      const rechecking = controller.recheckNewer('5')
      await new Promise((resolve) => setTimeout(resolve, 0))
      expect(client.view).toHaveBeenCalledTimes(1)

      release()
      await rechecking
      expect(controller.projection?.revision).toBe('5')
    })

    it('waits for a command-bar transition still on its way before rechecking', async () => {
      let answer!: (next: ViewProjection) => void
      const client = clientWith({
        view: vi
          .fn()
          .mockResolvedValueOnce(projection('v=1', 0, 3, '4'))
          .mockImplementation(async (body: ViewBody) => projection(body.query, 0, 3, '5')),
        transition: vi.fn(() => new Promise<ViewProjection>((resolve) => (answer = resolve))),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()

      const applying = controller.apply({ action: 'view.set-subgrouping', dimension: 'account' })
      await new Promise((resolve) => setTimeout(resolve, 0))
      const rechecking = controller.recheckNewer('5')
      await new Promise((resolve) => setTimeout(resolve, 0))
      answer(projection('v=2', 0, 3, '4'))

      await expect(applying).resolves.toBe(true)
      await rechecking
      expect(controller.projection?.canonical_query).toBe('v=2')
      expect(controller.projection?.revision).toBe('5')
    })
  })

  describe('reloading for a new day', () => {
    it('takes the same revision’s new rows and drops the windows read the day before', async () => {
      let total = 450
      const client = clientWith({
        view: vi.fn(async (body: ViewBody) => projection('v=1', body.window.offset, total, '4')),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      await controller.moveCursorTo(250)
      const calls = vi.mocked(client.view).mock.calls.length

      // The Time rows end at today, so the new day adds a row at the same revision.
      total = 451
      await controller.reloadDay()
      expect(client.view).toHaveBeenCalledTimes(calls + 1)
      expect(controller.projection?.total_rows).toBe(451)
      expect(controller.cursorIndex).toBe(250)

      await controller.moveCursorTo(10)
      expect(controller.projection?.window.offset).toBe(0)
      expect(controller.projection?.total_rows).toBe(451)
    })

    it('waits for a command-bar transition still on its way', async () => {
      let answer!: (next: ViewProjection) => void
      const client = clientWith({
        view: vi.fn(async (body: ViewBody) => projection(body.query, 0, 3, '4')),
        transition: vi.fn(() => new Promise<ViewProjection>((resolve) => (answer = resolve))),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()

      const applying = controller.apply({ action: 'view.set-subgrouping', dimension: 'account' })
      await new Promise((resolve) => setTimeout(resolve, 0))
      const reloading = controller.reloadDay()
      await new Promise((resolve) => setTimeout(resolve, 0))
      answer(projection('v=2', 0, 3, '4'))

      await expect(applying).resolves.toBe(true)
      await reloading
      expect(vi.mocked(client.view).mock.lastCall?.[0]).toMatchObject({ query: 'v=2' })
      expect(controller.projection?.canonical_query).toBe('v=2')
    })
  })

  describe('showing a scrolled-to row', () => {
    it('loads the aligned window outside the loaded one and moves the cursor into it', async () => {
      const client = clientWith({
        view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      const pushes = vi.spyOn(history, 'pushState')
      pushes.mockClear()
      const calls = vi.mocked(client.view).mock.calls.length

      await controller.showIndex(250)
      expect(client.view).toHaveBeenCalledTimes(calls + 1)
      expect(vi.mocked(client.view).mock.lastCall?.[0].window).toEqual({ offset: 200, limit: 200 })
      expect(controller.projection?.window.offset).toBe(200)
      expect(controller.cursorIndex).toBe(250)
      expect(controller.cursorIdentity).toBe('row-250')
      expect((history.state as MoneyflowHistoryState).cursorIndex).toBe(250)

      await controller.showIndex(320)
      expect(client.view).toHaveBeenCalledTimes(calls + 1)
      expect(controller.cursorIndex).toBe(250)

      await controller.showIndex(5)
      expect(client.view).toHaveBeenCalledTimes(calls + 1)
      expect(controller.projection?.window.offset).toBe(0)
      expect(controller.cursorIndex).toBe(5)
      expect(pushes).not.toHaveBeenCalled()
    })

    it('ignores rows outside the result', async () => {
      const client = clientWith({
        view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      const calls = vi.mocked(client.view).mock.calls.length

      await controller.showIndex(450)
      await controller.showIndex(-1)
      await controller.showIndex(1.5)
      expect(client.view).toHaveBeenCalledTimes(calls)
      expect(controller.cursorIndex).toBe(0)
    })

    it('moves the cursor to the latest row asked for while its window loads', async () => {
      let release!: (value: ViewProjection) => void
      const client = clientWith({
        view: vi.fn(async (body) => {
          if (body.window.offset === 0) return projection('v=1', 0, 450)
          return await new Promise<ViewProjection>((resolve) => (release = resolve))
        }),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      const calls = vi.mocked(client.view).mock.calls.length

      const first = controller.showIndex(250)
      const second = controller.showIndex(262)
      release(projection('v=1', 200, 450))
      await Promise.all([first, second])

      expect(client.view).toHaveBeenCalledTimes(calls + 1)
      expect(controller.cursorIndex).toBe(262)
    })

    it('withdraws a scrolled-to window when asked for a row of the loaded one', async () => {
      let release!: (value: ViewProjection) => void
      const client = clientWith({
        view: vi.fn(async (body) => {
          if (body.window.offset === 0) return projection('v=1', 0, 450)
          return await new Promise<ViewProjection>((resolve) => (release = resolve))
        }),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      await controller.moveCursorTo(50)

      const shown = controller.showIndex(250)
      await controller.showIndex(40)
      release(projection('v=1', 200, 450))
      await shown

      expect(controller.projection?.window.offset).toBe(0)
      expect(controller.cursorIndex).toBe(50)
    })

    it('keeps a newer transition’s view when the scrolled-to window arrives late', async () => {
      let release!: (value: ViewProjection) => void
      const client = clientWith({
        view: vi.fn(async (body) => {
          if (body.window.offset === 0) return projection('v=1', 0, 450)
          return await new Promise<ViewProjection>((resolve) => (release = resolve))
        }),
        transition: vi.fn(async () => projection('v=1&sort=amount', 0, 450)),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()

      const shown = controller.showIndex(250)
      await controller.apply({ action: 'sort.set', sort_field: 'amount' })
      release(projection('v=1', 200, 450))
      await shown

      expect(controller.projection?.canonical_query).toBe('v=1&sort=amount')
      expect(controller.projection?.window.offset).toBe(0)
      expect(controller.cursorIndex).toBe(0)
    })

    it.each([
      ['Home', (controller: ViewController) => controller.moveHome()],
      ['Up', (controller: ViewController) => controller.moveCursor(-1)],
    ])('drops a scrolled-to window when %s keeps the cursor on the first row', async (_, key) => {
      let release!: (value: ViewProjection) => void
      const client = clientWith({
        view: vi.fn(async (body) => {
          if (body.window.offset === 0) return projection('v=1', 0, 450)
          return await new Promise<ViewProjection>((resolve) => (release = resolve))
        }),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()

      const shown = controller.showIndex(250)
      await key(controller)
      release(projection('v=1', 200, 450))
      await shown

      expect(controller.projection?.window.offset).toBe(0)
      expect(controller.cursorIndex).toBe(0)
    })

    it('drops a scrolled-to window once a key moves the cursor, and can load it again', async () => {
      const releases: ((value: ViewProjection) => void)[] = []
      const client = clientWith({
        view: vi.fn(async (body) => {
          if (body.window.offset === 0) return projection('v=1', 0, 450)
          return await new Promise<ViewProjection>((resolve) => releases.push(resolve))
        }),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      const calls = vi.mocked(client.view).mock.calls.length

      const shown = controller.showIndex(250)
      await controller.moveCursor(1)
      const again = controller.showIndex(255)
      releases[0]!(projection('v=1', 200, 450))
      await shown
      expect(controller.projection?.window.offset).toBe(0)
      expect(controller.cursorIndex).toBe(1)

      expect(client.view).toHaveBeenCalledTimes(calls + 2)
      releases[1]!(projection('v=1', 200, 450))
      await again
      expect(controller.cursorIndex).toBe(255)
    })

    it('reports a scrolled-to window that fails to load, and retries it', async () => {
      const client = clientWith({
        view: vi
          .fn()
          .mockImplementationOnce(async () => projection('v=1', 0, 450))
          .mockRejectedValueOnce(new TypeError('Failed to fetch'))
          .mockImplementation(async (body) => projection('v=1', body.window.offset, 450)),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()

      await controller.showIndex(250)
      expect(controller.problem).toMatchObject({ kind: 'request', code: 'network_error' })
      expect(controller.projection?.window.offset).toBe(0)

      await controller.retry()
      expect(controller.problem).toBeUndefined()
      expect(controller.projection?.window.offset).toBe(200)
      expect(controller.cursorIndex).toBe(250)
    })

    it('clears the load error when a retry finds the window a prefetch cached', async () => {
      let prefetched!: (value: ViewProjection) => void
      let requests = 0
      const client = clientWith({
        view: vi.fn(async (body) => {
          if (body.window.offset !== 200) return projection('v=1', body.window.offset, 450)
          requests += 1
          if (requests === 1) return await new Promise<ViewProjection>((r) => (prefetched = r))
          throw new TypeError('Failed to fetch')
        }),
      })
      const controller = controllerFor(client)
      await controller.hydrate()
      await vi.waitFor(() => expect(requests).toBe(1))

      await controller.showIndex(250)
      expect(controller.problem).toMatchObject({ kind: 'request', code: 'network_error' })
      prefetched(projection('v=1', 200, 450))
      await vi.waitFor(() =>
        expect(controller.neighbors.map((loaded) => loaded.window.offset)).toEqual([200]),
      )

      await controller.retry()
      expect(controller.projection?.window.offset).toBe(200)
      expect(controller.cursorIndex).toBe(250)
      expect(controller.problem).toBeUndefined()
    })

    it('lets a transition already running finish', async () => {
      let release!: (value: ViewProjection) => void
      const client = clientWith({
        view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
        transition: vi.fn(async () => await new Promise<ViewProjection>((r) => (release = r))),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()

      const applied = controller.apply({ action: 'sort.set', sort_field: 'amount' })
      await vi.waitFor(() => expect(client.transition).toHaveBeenCalled())
      await controller.showIndex(250)
      release(projection('v=1&sort=amount', 0, 450))

      await expect(applied).resolves.toBe(true)
      expect(vi.mocked(client.transition).mock.lastCall?.[1]?.aborted).toBe(false)
      expect(controller.projection?.canonical_query).toBe('v=1&sort=amount')
    })

    it('keeps the live window’s neighbors when an older prefetch lands late', async () => {
      const held = new Map<number, (value: ViewProjection) => void>()
      const client = clientWith({
        view: vi.fn(async (body) => {
          const offset = body.window.offset
          if (offset === 200 || offset === 1000) {
            return await new Promise<ViewProjection>((resolve) =>
              held.set(offset, (value) => resolve(value)),
            )
          }
          return projection('v=1', offset, 1200)
        }),
      })
      const controller = controllerFor(client)
      await controller.hydrate()
      await vi.waitFor(() => expect(held.has(200)).toBe(true))

      await controller.showIndex(900)
      await vi.waitFor(() => expect(held.has(1000)).toBe(true))
      held.get(1000)!(projection('v=1', 1000, 1200))
      await vi.waitFor(() =>
        expect(controller.neighbors.map((loaded) => loaded.window.offset)).toContain(1000),
      )
      held.get(200)!(projection('v=1', 200, 1200))
      await new Promise((resolve) => setTimeout(resolve, 0))

      expect(controller.projection?.window.offset).toBe(800)
      expect(controller.neighbors.map((loaded) => loaded.window.offset)).toContain(1000)
    })

    it('prefetches past a cached window the cursor moves into', async () => {
      const client = clientWith({
        view: vi.fn(async (body) => projection('v=1', body.window.offset, 1200)),
      })
      const controller = controllerFor(client)
      await controller.hydrate()
      await vi.waitFor(() =>
        expect(controller.neighbors.map((loaded) => loaded.window.offset)).toEqual([200]),
      )

      await controller.moveCursorTo(199)
      await controller.moveCursor(1)
      expect(controller.projection?.window.offset).toBe(200)
      expect(controller.cursorIndex).toBe(200)
      await vi.waitFor(() =>
        expect(controller.neighbors.map((loaded) => loaded.window.offset)).toEqual([0, 400]),
      )
    })

    it('exposes the cached windows next to the loaded one', async () => {
      const client = clientWith({
        view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
      })
      const controller = controllerFor(client)
      await controller.hydrate()
      await vi.waitFor(() =>
        expect(controller.neighbors.map((loaded) => loaded.window.offset)).toEqual([200]),
      )

      await controller.showIndex(250)
      await vi.waitFor(() =>
        expect(controller.neighbors.map((loaded) => loaded.window.offset)).toEqual([0, 400]),
      )
    })
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
      detail: 'The selection is too large.',
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
    expect(controller.announcement).toContain('The selection is too large.')
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

    expect(await controller.previewSearch('coffee')).toBe('applied')
    expect(await controller.previewSearch('coffee shop')).toBe('applied')
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

  it('runs a transition after held work, such as a search committed on blur', async () => {
    const client = clientWith({
      transition: vi.fn(async (body) =>
        body.action === 'search.apply'
          ? projection(`v=1&q=${encodeURIComponent(body.search ?? '')}`, 0, 1)
          : projection(`${body.query}&group=category`, 0, 1),
      ),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    const snapshot = controller.beginSearch()
    let finish!: () => void
    const held = controller.hold(
      new Promise<void>((resolve) => (finish = resolve)).then(async () => {
        await controller.previewSearch('coffee')
        controller.commitSearch(snapshot)
      }),
    )

    const grouping = controller.apply({ action: 'view.cycle-grouping' })
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(client.transition).not.toHaveBeenCalled()
    finish()
    await Promise.all([held, grouping])

    expect(vi.mocked(client.transition).mock.calls.map(([body]) => body.action)).toEqual([
      'search.apply',
      'view.cycle-grouping',
    ])
    expect(controller.projection?.canonical_query).toBe('v=1&q=coffee&group=category')
  })

  it('ignores a search commit or restore after browser history drops its snapshot', async () => {
    const client = clientWith({
      view: vi.fn(async (body) => projection(body.query, body.window.offset, 3)),
      transition: vi.fn(async (body) =>
        projection(`v=1&q=${encodeURIComponent(body.search ?? '')}`, 0, 1),
      ),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    const snapshot = controller.beginSearch()
    await controller.previewSearch('coffee')
    const earlier = {
      ...(history.state as MoneyflowHistoryState),
      query: 'group=category&v=1',
      sequence: 4,
    }
    await controller.restore(new PopStateEvent('popstate', { state: earlier }))
    const pushes = vi.spyOn(history, 'pushState')
    const replacements = vi.spyOn(history, 'replaceState')
    pushes.mockClear()
    replacements.mockClear()

    controller.commitSearch(snapshot)
    controller.restoreSearch(snapshot)

    expect(pushes).not.toHaveBeenCalled()
    expect(replacements).not.toHaveBeenCalled()
    expect(controller.projection?.canonical_query).toBe('group=category&v=1')
  })

  it('tells a rejected search preview from one a newer request superseded', async () => {
    let release!: (value: ViewProjection) => void
    const transition = vi
      .fn()
      .mockImplementationOnce(async () => {
        throw new MoneyflowProblem({
          type: 'about:blank',
          title: 'Bad Request',
          status: 400,
          detail: 'The web view request is invalid.',
          code: 'invalid_web_request',
        })
      })
      .mockImplementationOnce(() => new Promise<ViewProjection>((resolve) => (release = resolve)))
      .mockImplementation(async () => projection('v=1&q=b', 0, 1))
    const controller = controllerFor(clientWith({ transition }), false)
    await controller.hydrate()
    controller.beginSearch()

    expect(await controller.previewSearch('[')).toBe('invalid')
    const slow = controller.previewSearch('a')
    expect(await controller.previewSearch('b')).toBe('applied')
    release(projection('v=1&q=a', 0, 1))
    expect(await slow).toBe('superseded')
  })

  it('reports a search preview the network failed with the connection reason', async () => {
    const transition = vi.fn(async () => {
      throw new TypeError('Failed to fetch')
    })
    const controller = controllerFor(clientWith({ transition }), false)
    await controller.hydrate()
    controller.beginSearch()

    expect(await controller.previewSearch('coffee')).toEqual({
      failed: 'Moneyflow could not reach the server. Check the connection and retry.',
    })
    expect(controller.problem).toMatchObject({ kind: 'request', code: 'network_error' })
  })

  it('reports a search preview the server refused for another reason with that reason', async () => {
    const transition = vi.fn(async () => {
      throw new MoneyflowProblem({
        type: 'about:blank',
        title: 'Service Unavailable',
        status: 503,
        detail: 'The profile is busy. Retry in a moment.',
        code: 'store_busy',
      })
    })
    const controller = controllerFor(clientWith({ transition }), false)
    await controller.hydrate()
    controller.beginSearch()

    expect(await controller.previewSearch('coffee')).toEqual({
      failed: 'The profile is busy. Retry in a moment.',
    })
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

    expect(controller.problem).toMatchObject({ kind: 'invalid-view', code: 'invalid_view_state' })
    expect(controller.announcement).toBe('This Moneyflow link is invalid.')
  })

  describe('the cursor after a transition', () => {
    /** The result laid out again, with each row two places above where it was. */
    function relaid(query: string, offset: number): ViewProjection {
      const next = projection(query, offset, 450)
      next.detail_rows = next.detail_rows!.map((row) => ({
        ...row,
        identity: `row-${row.index + 2}`,
      }))
      return next
    }

    async function cursorOnRow230() {
      const client = clientWith({
        view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
        transition: vi.fn(async (body) => relaid(`v=1&after=${body.action}`, body.window.offset)),
      })
      const controller = controllerFor(client, false)
      await controller.hydrate()
      await controller.moveCursorTo(230)
      expect(controller.cursorIdentity).toBe('row-230')
      return { client, controller }
    }

    const rowsChange = [
      { action: 'time.set-period', period: { start: '2026-09-01', end: '2026-09-30' } },
      { action: 'time.previous-period' },
      { action: 'time.next-period' },
      { action: 'time.clear-period' },
      { action: 'view.drill', target: { identity: 'row-230', kind: 'aggregate' } },
      { action: 'view.set-grouping', dimension: 'category' },
      { action: 'view.cycle-grouping' },
      { action: 'view.set-subgrouping', dimension: 'account' },
      { action: 'view.show-detail' },
      { action: 'view.switch-accounts' },
      { action: 'view.set-currency', currency: { currency: 'EUR', scale: 2 } },
      { action: 'search.apply', search: 'coffee' },
      { action: 'filters.apply', filters: { show_hidden: true, show_transfers: false } },
    ] as const

    it.each(rowsChange)(
      'starts on the first row after $action, as the TUI does',
      async (request) => {
        const { client, controller } = await cursorOnRow230()

        await controller.apply(request)

        expect(controller.cursorIndex).toBe(0)
        expect(controller.cursorIdentity).toBe('row-2')
        expect(client.transition).toHaveBeenLastCalledWith(
          expect.objectContaining({ window: { offset: 0, limit: 200 } }),
          expect.any(AbortSignal),
        )
      },
    )

    it('starts a search preview on the first row', async () => {
      const { controller } = await cursorOnRow230()
      controller.beginSearch()

      await expect(controller.previewSearch('coffee')).resolves.toBe('applied')

      expect(controller.cursorIndex).toBe(0)
    })

    it.each([
      { action: 'sort.set', sort_field: 'amount' },
      { action: 'sort.cycle' },
      { action: 'sort.reverse' },
      { action: 'selection.toggle', target: { identity: 'row-230', kind: 'transaction' } },
    ] as const)('keeps the cursor on its row after $action', async (request) => {
      const { controller } = await cursorOnRow230()

      await controller.apply(request)

      expect(controller.cursorIdentity).toBe('row-230')
      expect(controller.cursorIndex).toBe(228)
    })
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

  it.each([
    [
      { action: 'view.set-grouping', dimension: 'category' },
      'group=category&v=1',
      'mfsel1.example',
    ],
    [
      { action: 'selection.toggle', target: { kind: 'transaction', identity: 'row-0' } },
      'v=1',
      'mfsel1.selected',
    ],
  ] as const)(
    'keeps a view change made while a provider refresh runs (%o)',
    async (action, query, selection) => {
      const refreshCapability = {
        id: 'provider.refresh',
        key_display: 'r',
        description: 'Refresh provider data',
        category: 'System',
        available: true,
      }
      let revision = '1'
      const shown = (next: ViewProjection) => ({ ...next, capabilities: [refreshCapability] })
      let finish: (response: Response) => void = () => undefined
      const client = clientWith({
        view: vi.fn(async (body) =>
          shown(projection(body.query, body.window.offset, 3, revision, body.selection)),
        ),
        transition: vi.fn(async () => shown(projection(query, 0, 3, revision, selection))),
        mutations: {
          request: vi.fn(() => new Promise<Response>((resolve) => (finish = resolve))),
        },
      })
      history.replaceState(null, '', '/moneyflow/?v=1')
      const controller = controllerFor(client, false)
      await controller.hydrate()

      const refreshing = controller.provider.refresh()
      await controller.apply(action)
      revision = '2'
      finish(Response.json(providerRefreshResponse(projection('v=1', 0, 3, '2'))))
      await expect(refreshing).resolves.toBe(true)

      expect(controller.projection?.canonical_query).toBe(query)
      expect(controller.projection?.selection).toBe(selection)
      expect(controller.projection?.revision).toBe('2')
      expect((history.state as MoneyflowHistoryState).query).toBe(query)
    },
  )

  it('lets a view change still on its way finish before a provider refresh lands', async () => {
    const refreshCapability = {
      id: 'provider.refresh',
      key_display: 'r',
      description: 'Refresh provider data',
      category: 'System',
      available: true,
    }
    let revision = '1'
    const shown = (next: ViewProjection) => ({ ...next, capabilities: [refreshCapability] })
    let finishRefresh: (response: Response) => void = () => undefined
    let finishTransition: () => void = () => undefined
    const client = clientWith({
      view: vi.fn(async (body) =>
        shown(projection(body.query, body.window.offset, 3, revision, body.selection)),
      ),
      transition: vi.fn(async (body) => {
        await new Promise<void>((resolve) => (finishTransition = resolve))
        return shown(projection('group=category&v=1', 0, 3, revision, body.selection))
      }),
      mutations: {
        request: vi.fn(() => new Promise<Response>((resolve) => (finishRefresh = resolve))),
      },
    })
    history.replaceState(null, '', '/moneyflow/?v=1')
    const controller = controllerFor(client, false)
    await controller.hydrate()

    const refreshing = controller.provider.refresh()
    const grouping = controller.apply({ action: 'view.set-grouping', dimension: 'category' })
    await vi.waitFor(() => expect(client.transition).toHaveBeenCalledOnce())
    revision = '2'
    finishRefresh(Response.json(providerRefreshResponse(projection('v=1', 0, 3, '2'))))
    await new Promise((resolve) => setTimeout(resolve, 0))
    finishTransition()

    await expect(grouping).resolves.toBe(true)
    await expect(refreshing).resolves.toBe(true)
    expect(controller.projection?.canonical_query).toBe('group=category&v=1')
    expect(controller.projection?.revision).toBe('2')
    expect((history.state as MoneyflowHistoryState).query).toBe('group=category&v=1')
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

describe('visible view failures', () => {
  beforeEach(() => {
    history.replaceState(null, '', '/moneyflow/?v=1')
    dismissFlash()
  })
  afterEach(() => dismissFlash())

  const flashes = () => getFlashes().map(({ message, tone }) => ({ message, tone }))
  const busy = () =>
    new MoneyflowProblem({
      type: 'about:blank',
      title: 'Service Unavailable',
      status: 503,
      detail: 'The profile is busy. Try again.',
      code: 'store_busy',
    })

  it('shows a refused view change with the server’s reason and keeps the view', async () => {
    const client = clientWith({ transition: vi.fn().mockRejectedValueOnce(busy()) })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    const prior = controller.projection

    await controller.apply({ action: 'view.set-subgrouping', dimension: 'account' })

    expect(controller.projection).toBe(prior)
    expect(controller.problem).toEqual({
      kind: 'request',
      code: 'store_busy',
      detail: 'The profile is busy. Try again.',
    })
    const message =
      'Moneyflow could not apply that change; the view is unchanged. ' +
      'The profile is busy. Try again.'
    expect(controller.announcement).toBe(message)
    expect(flashes()).toEqual([{ message, tone: 'danger' }])
  })

  it('shows why a cursor move to a row outside the loaded window failed', async () => {
    const client = clientWith({
      view: vi
        .fn()
        .mockImplementationOnce(async () => projection('v=1', 0, 450))
        .mockRejectedValueOnce(busy()),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()

    await controller.moveCursorTo(250)

    expect(controller.cursorIndex).toBe(0)
    expect(flashes()).toEqual([
      {
        message:
          'Moneyflow could not apply that change; the view is unchanged. ' +
          'The profile is busy. Try again.',
        tone: 'danger',
      },
    ])
  })

  it('points an unexpected server error to the server log', async () => {
    const failure = new MoneyflowProblem({
      type: 'about:blank',
      title: 'Internal Server Error',
      status: 500,
      detail: 'The request could not be completed.',
      code: 'internal_error',
    })
    const client = clientWith({ transition: vi.fn().mockRejectedValueOnce(failure) })
    const controller = controllerFor(client, false)
    await controller.hydrate()

    await controller.apply({ action: 'view.set-subgrouping', dimension: 'account' })

    expect(controller.problem?.detail).toBe(
      'The request could not be completed. The terminal running moneyflow web shows the cause.',
    )
  })

  it('tells an unreadable answer from a network failure', async () => {
    const client = clientWith({
      transition: vi
        .fn()
        .mockRejectedValueOnce(new Error('The Moneyflow API returned an invalid response.'))
        .mockRejectedValueOnce(new TypeError('Failed to fetch')),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()

    await controller.apply({ action: 'view.set-subgrouping', dimension: 'account' })
    expect(controller.problem).toEqual({
      kind: 'request',
      code: 'invalid_response',
      detail: 'The server sent a response Moneyflow could not read.',
    })
    await controller.apply({ action: 'view.set-subgrouping', dimension: 'account' })
    expect(controller.problem?.code).toBe('network_error')
  })

  it('leaves a failed first load to the page that replaces the workspace', async () => {
    const client = clientWith({ view: vi.fn().mockRejectedValueOnce(busy()) })
    const controller = controllerFor(client, false)

    await controller.hydrate()

    expect(controller.problem?.detail).toBe('The profile is busy. Try again.')
    expect(flashes()).toEqual([])
  })

  it('reports a failed recheck, and a retry that succeeds clears it', async () => {
    const view = vi
      .fn()
      .mockResolvedValueOnce(projection('v=1', 0, 3, '4'))
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce(projection('v=1', 0, 3, '4'))
    const controller = controllerFor(clientWith({ view }), false)
    await controller.hydrate()

    await expect(controller.recheck()).resolves.toBeUndefined()

    expect(controller.problem?.code).toBe('network_error')
    expect(flashes()).toEqual([
      {
        message:
          'Moneyflow could not check for newer data; the view may be out of date. ' +
          'Moneyflow could not reach the server. Check the connection and retry.',
        tone: 'danger',
      },
    ])
    await controller.retry()
    expect(view).toHaveBeenCalledTimes(3)
    expect(controller.problem).toBeUndefined()
  })

  it('shows the parent view undo returned to when the drilled target is gone', async () => {
    history.replaceState(null, '', '/moneyflow/?v=1&drill=x')
    const notice = 'The previous view target is unavailable; returned to its parent.'
    const parent = {
      ...projection('v=1', 0, 3, '5'),
      warnings: [{ code: 'view_notice', detail: notice }],
    }
    // The server cannot project the drilled view any more, so a reread of it fails.
    const stale = new MoneyflowProblem({
      type: 'about:blank',
      title: 'Conflict',
      status: 409,
      detail: 'The selected view target is no longer available.',
      code: 'stale_view_target',
    })
    const view = vi
      .fn()
      .mockResolvedValueOnce(projection('v=1&drill=x', 0, 3, '4'))
      .mockRejectedValue(stale)
    const mutations = {
      request: vi.fn(async () =>
        Response.json({
          version: '1',
          revision: '5',
          canonical_query: 'v=1',
          projection: parent,
          pending: parent.pending,
          selection: { kind: 'preserved', value: parent.selection },
        }),
      ),
    }
    const controller = controllerFor(clientWith({ view, mutations }), false)
    await controller.hydrate()

    await expect(controller.editing.undo()).resolves.toBe(true)

    expect(controller.projection?.canonical_query).toBe('v=1')
    expect(controller.projection?.revision).toBe('5')
    expect(location.search).toBe('?v=1')
    expect(flashes().map(({ message }) => message)).toEqual([notice])
  })

  it('ignores a recheck that fails after a newer transition failed', async () => {
    let failRecheck!: (error: unknown) => void
    const view = vi
      .fn()
      .mockResolvedValueOnce(projection('v=1', 0, 3, '4'))
      .mockImplementationOnce(() => new Promise((_, reject) => (failRecheck = reject)))
    const transition = vi.fn().mockRejectedValueOnce(busy())
    const controller = controllerFor(clientWith({ view, transition }), false)
    await controller.hydrate()

    const rechecking = controller.recheck()
    await controller.apply({ action: 'view.set-subgrouping', dimension: 'account' })
    failRecheck(new TypeError('Failed to fetch'))
    await expect(rechecking).resolves.toBeUndefined()

    expect(controller.problem?.code).toBe('store_busy')
    expect(flashes().map(({ message }) => message)).toEqual([
      'Moneyflow could not apply that change; the view is unchanged. ' +
        'The profile is busy. Try again.',
    ])
    await controller.retry()
    expect(transition).toHaveBeenCalledTimes(2)
    expect(view).toHaveBeenCalledTimes(2)
  })

  it('ignores a recheck that fails after a newer window load failed', async () => {
    let failRecheck!: (error: unknown) => void
    const view = vi
      .fn()
      .mockResolvedValueOnce(projection('v=1', 0, 600, '4'))
      .mockImplementationOnce(() => new Promise((_, reject) => (failRecheck = reject)))
      .mockRejectedValueOnce(busy())
      .mockResolvedValueOnce(projection('v=1', 200, 600, '4'))
    const controller = controllerFor(clientWith({ view }), false)
    await controller.hydrate()

    const rechecking = controller.recheck()
    await controller.showIndex(250)
    expect(controller.problem?.code).toBe('store_busy')
    failRecheck(new TypeError('Failed to fetch'))
    await expect(rechecking).resolves.toBeUndefined()

    expect(controller.problem?.code).toBe('store_busy')
    await controller.retry()
    expect(view).toHaveBeenCalledTimes(4)
    expect(view.mock.calls[3]![0]).toMatchObject({ window: { offset: 200 } })
    expect(controller.problem).toBeUndefined()
  })

  it('ignores a recheck that fails after a newer transition succeeded', async () => {
    let failRecheck!: (error: unknown) => void
    const view = vi
      .fn()
      .mockResolvedValueOnce(projection('v=1', 0, 3, '4'))
      .mockImplementationOnce(() => new Promise((_, reject) => (failRecheck = reject)))
    const controller = controllerFor(clientWith({ view }), false)
    await controller.hydrate()

    const rechecking = controller.recheck()
    await controller.apply({ action: 'view.set-subgrouping', dimension: 'account' })
    failRecheck(new TypeError('Failed to fetch'))
    await expect(rechecking).resolves.toBeUndefined()

    expect(controller.problem).toBeUndefined()
    expect(flashes()).toEqual([])
  })

  it('reports a failed recheck for newer insights as a view failure', async () => {
    const view = vi
      .fn()
      .mockResolvedValueOnce(projection('v=1', 0, 3, '4'))
      .mockRejectedValueOnce(busy())
    const controller = controllerFor(clientWith({ view }), false)
    await controller.hydrate()

    await controller.recheckNewer('5')

    expect(controller.problem?.code).toBe('store_busy')
    expect(flashes().map(({ tone }) => tone)).toEqual(['danger'])
  })

  it('does not show a refused search preview in a banner', async () => {
    const client = clientWith({ transition: vi.fn().mockRejectedValueOnce(busy()) })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    controller.beginSearch()

    await controller.previewSearch('coffee')

    expect(flashes()).toEqual([])
  })

  it('shows the warnings of a view the user now sees, except the audit warning', async () => {
    const client = clientWith({
      transition: vi.fn(async (body) => ({
        ...projection(body.query, 0, 3),
        warnings: [
          { code: 'view_notice', detail: 'The selection changed and was cleared after refresh.' },
          { code: 'audit_completion_failed', detail: 'Changes saved, but the audit failed.' },
        ],
      })),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()

    await controller.apply({ action: 'view.set-subgrouping', dimension: 'account' })

    expect(flashes()).toEqual([
      { message: 'The selection changed and was cleared after refresh.', tone: 'warning' },
    ])
  })

  it('shows why transaction information did not load', async () => {
    const client = clientWith({ transactionInformation: vi.fn().mockRejectedValueOnce(busy()) })
    const controller = controllerFor(client, false)
    await controller.hydrate()

    await expect(controller.transactionInformation?.('row-0')).resolves.toBeUndefined()

    expect(flashes()).toEqual([
      {
        message: 'Transaction information could not be loaded. The profile is busy. Try again.',
        tone: 'danger',
      },
    ])
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
    historyDebounceMillis: 0,
    returnTimeoutMillis: 200,
  })
}

/** A controller that defers cursor history writes, as the browser does. */
function debouncedControllerFor(client: MoneyflowClient) {
  return createViewController({
    basePath: '/moneyflow/',
    client,
    history,
    location: window.location,
    instance: 'test-instance',
    prefetch: false,
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
    insights: vi.fn(async () => {
      throw new Error('insights not stubbed')
    }),
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
      sort_fields: ['date', 'merchant', 'category', 'account', 'amount'],
      drill_depth: 0,
      root: { mode: 'detail', grouping: 'merchant', time_granularity: 'year' },
    },
    selection: selection as ViewProjection['selection'],
    selection_count: 0,
    all_rows_selected: false,
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
