import { beforeEach, describe, expect, it, vi } from 'vitest'

import { MoneyflowProblem, type MoneyflowClient, type ViewProjection } from '../api/client'
import { createViewController } from './view-controller.svelte'
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

  it('pushes analytical transitions and replaces selection-only transitions', async () => {
    const pushes = vi.spyOn(history, 'pushState')
    const replacements = vi.spyOn(history, 'replaceState')
    const client = clientWith({
      transition: async (body) =>
        projection(body.action === 'group.cycle' ? 'v=1&group=category' : body.query, 0, 3),
    })
    const controller = controllerFor(client)
    await controller.hydrate()
    pushes.mockClear()
    replacements.mockClear()

    await controller.apply({ action: 'group.cycle' })
    expect(pushes).toHaveBeenCalledTimes(1)
    expect(window.location.search).toBe('?v=1&group=category')

    await controller.apply({ action: 'select.one', target: { kind: 'detail', identity: 'row-0' } })
    expect(pushes).toHaveBeenCalledTimes(1)
    expect(replacements).toHaveBeenCalled()
  })

  it('uses verified owned history for Esc and replaces with the server parent otherwise', async () => {
    const go = vi.spyOn(history, 'go').mockImplementation(() => undefined)
    const controller = controllerFor(
      clientWith({
        transition: async () => projection('v=1', 0, 2),
      }),
    )
    await controller.hydrate()
    await controller.apply({ action: 'group.cycle' })
    await controller.apply({ action: 'nav.back' })
    expect(go).toHaveBeenCalledWith(-1)

    go.mockClear()
    history.replaceState({ owner: 'foreign' }, '', '/moneyflow/?v=1&group=category')
    await controller.apply({ action: 'nav.back' })
    expect(go).not.toHaveBeenCalled()
    expect(window.location.search).toBe('?v=1')
  })

  it('restores owned Back/Forward state including cursor and selection', async () => {
    const client = clientWith()
    const controller = controllerFor(client)
    await controller.hydrate()
    const state = {
      ...(history.state as MoneyflowHistoryState),
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
  })

  it('moves synchronously in a loaded window and fetches an uncached boundary window', async () => {
    const client = clientWith({
      view: vi.fn(async (body) => projection('v=1', body.window.offset, 450)),
    })
    const controller = controllerFor(client, false)
    await controller.hydrate()
    const calls = vi.mocked(client.view).mock.calls.length

    await controller.moveCursor(1)
    expect(controller.cursorIndex).toBe(1)
    expect(client.view).toHaveBeenCalledTimes(calls)

    for (let index = 1; index < 200; index += 1) await controller.moveCursor(1)
    expect(controller.cursorIndex).toBe(200)
    expect(client.view).toHaveBeenCalledTimes(calls + 1)

    await controller.moveHome()
    expect(controller.cursorIndex).toBe(0)
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

    await controller.apply({ action: 'select.all' })
    expect(controller.projection).toBe(prior)
    expect(controller.announcement).not.toContain('private')
    await controller.retry()
    expect(client.transition).toHaveBeenCalledTimes(2)
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
})

function controllerFor(client: MoneyflowClient, prefetch = true) {
  return createViewController({
    basePath: '/moneyflow/',
    client,
    history,
    location: window.location,
    instance: 'test-instance',
    prefetch,
  })
}

function clientWith(overrides: Partial<MoneyflowClient> = {}): MoneyflowClient {
  return {
    view: vi.fn(async (body) => projection('v=1', body.window.offset, 3)),
    transition: vi.fn(async (body) => projection(body.query, body.window.offset, 3)),
    ...overrides,
  }
}

function projection(query: string, offset: number, total: number): ViewProjection {
  const count = Math.max(0, Math.min(200, total - offset))
  return {
    api_schema_version: '1',
    projection_schema_version: '1',
    canonical_query: query,
    selection: 'mfsel1.example' as ViewProjection['selection'],
    breadcrumbs: [],
    breadcrumb_text: 'All transactions',
    filters: { show_hidden: false, show_transfers: false },
    capabilities: [],
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
