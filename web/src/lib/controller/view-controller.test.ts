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
      target: { kind: 'detail', identity: 'row-0' },
    })
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
    await controller.apply({ action: 'view.cycle-grouping' })
    await controller.apply({ action: 'view.back' })
    expect(go).toHaveBeenCalledWith(-1)

    go.mockClear()
    history.replaceState({ owner: 'foreign' }, '', '/moneyflow/?v=1&group=category')
    await controller.apply({ action: 'view.back' })
    expect(go).not.toHaveBeenCalled()
    expect(window.location.search).toBe('?v=1')
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
  })
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
    ...overrides,
  }
}

function projection(query: string, offset: number, total: number, revision = '0'): ViewProjection {
  const count = Math.max(0, Math.min(200, total - offset))
  return {
    api_schema_version: '1',
    projection_schema_version: '1',
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
    selection: 'mfsel1.example' as ViewProjection['selection'],
    selection_count: 0,
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
