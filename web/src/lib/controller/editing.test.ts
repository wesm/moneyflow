import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  MoneyflowProblem,
  type MutationFetch,
  type OperationSummary,
  type SelectionValue,
  type ViewProjection,
} from '../api/client'
import { notices } from '../../test/notices'
import { testProjection } from '../../test/projection'
import { createEditingController, type EditingProjectionHost } from './editing'

describe('browser editing controller', () => {
  beforeEach(() => history.replaceState({ safe: true }, '', '/moneyflow/?v=1&group=merchant'))

  it('submits exact revisions and installs the server projection without browser history writes', async () => {
    const localStorageWrite = vi.fn()
    const sessionStorageWrite = vi.fn()
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      value: { setItem: localStorageWrite },
    })
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: { setItem: sessionStorageWrite },
    })
    const pushes = vi.spyOn(history, 'pushState')
    const replacements = vi.spyOn(history, 'replaceState')
    const host = editingHost(
      testProjection({ revision: '7', canonical_query: 'v=1&group=merchant' }),
    )
    const transport = mutationTransport(
      mutationResponse('8', 'mfsel1.cleared' as SelectionValue, {
        active_operations: 1,
        inactive_operations: 0,
        affected_transactions: 2,
      }),
    )
    const controller = createEditingController({ transport, host })

    expect(controller.state.revision).toBe(7n)
    await expect(
      controller.submit({
        action: 'edit.merchant',
        input: { scope: 'entity', label: 'Normalized Merchant' },
        target: { kind: 'aggregate', identity: 'merchant-1' },
      }),
    ).resolves.toBe(true)

    expect(transport.request).toHaveBeenCalledTimes(1)
    const [path, rawBody] = vi.mocked(transport.request).mock.calls[0]!
    expect(path).toBe('api/v1/mutations')
    expect(JSON.parse(rawBody)).toMatchObject({
      version: '1',
      expected_revision: '7',
      query: 'v=1&group=merchant',
      action: 'edit.merchant',
      input: { scope: 'entity', label: 'Normalized Merchant' },
    })
    expect(controller.state.revision).toBe(8n)
    expect(controller.state.pending.active_operations).toBe(1)
    expect(host.current()?.selection).toBe('mfsel1.cleared')
    expect(window.location.search).toBe('?v=1&group=merchant')
    expect(history.state).toEqual({ safe: true })
    expect(JSON.stringify(history.state)).not.toContain('Normalized Merchant')
    expect(localStorageWrite).not.toHaveBeenCalled()
    expect(sessionStorageWrite).not.toHaveBeenCalled()
    expect(pushes).not.toHaveBeenCalled()
    expect(replacements).not.toHaveBeenCalled()
  })

  describe('toasts', () => {
    async function respondWith(
      operation: OperationSummary,
      run: (controller: ReturnType<typeof createEditingController>) => Promise<boolean>,
    ) {
      const response = { ...mutationResponse('8', 'mfsel1.x' as SelectionValue), operation }
      const controller = createEditingController({
        transport: mutationTransport(response),
        host: editingHost(
          testProjection({ revision: '7', canonical_query: response.canonical_query }),
        ),
      })
      await run(controller)
      return controller
    }

    const hide: OperationSummary = {
      kind: 'hide',
      label: 'Hide transactions',
      affected: 2,
      undoable: true,
    }
    const target = { kind: 'transaction', identity: 'transaction-1' }

    it('shows one toast for a hide and announces the same text', async () => {
      const controller = await respondWith(hide, (c) =>
        c.submit({ action: 'transaction.toggle-hidden', input: {}, target }),
      )

      expect(notices()).toEqual({ banners: [], status: 'Hidden 2 transactions · Undo u' })
      expect(controller.state.announcement).toBe('Hidden 2 transactions · Undo u')
    })

    it('announces a show and a delete with their toast text', async () => {
      const shown = await respondWith({ ...hide, kind: 'show', label: 'Show transactions' }, (c) =>
        c.submit({ action: 'transaction.toggle-hidden', input: {}, target }),
      )
      expect(shown.state.announcement).toBe('Shown 2 transactions · Undo u')
      const deleted = await respondWith(
        { ...hide, kind: 'delete', label: 'Delete transaction', affected: 1 },
        (c) => c.submit({ action: 'transaction.delete', input: {}, target }),
      )
      expect(deleted.state.announcement).toBe('Deleted 1 transaction · Undo u')
    })

    it('toasts an undo by the kind of the undone edit', async () => {
      const controller = await respondWith(
        { ...hide, kind: 'delete', label: 'Delete transaction', affected: 1 },
        (c) => c.undo(),
      )

      expect(notices()).toEqual({ banners: [], status: 'Undid delete · 1 transaction · Redo U' })
      expect(controller.state.announcement).toBe('Undid delete · 1 transaction · Redo U')
    })

    it('keeps the warning of the view an undo returned to beside its toast', async () => {
      const notice = 'The previous view target is unavailable; returned to its parent.'
      const response = {
        ...mutationResponse('8', 'mfsel1.x' as SelectionValue),
        operation: { ...hide, kind: 'delete', label: 'Delete transaction', affected: 1 },
      }
      response.projection.warnings = [{ code: 'view_notice', detail: notice }]
      const controller = createEditingController({
        transport: mutationTransport(response),
        host: editingHost(
          testProjection({ revision: '7', canonical_query: response.canonical_query }),
        ),
      })

      await controller.undo()

      expect(notices()).toEqual({
        banners: [],
        status: `Undid delete · 1 transaction · Redo U ${notice}`,
      })
    })

    it('toasts and announces a redo', async () => {
      const controller = await respondWith(hide, (c) => c.redo())

      expect(notices()).toEqual({ banners: [], status: 'Redid hide · 2 transactions · Undo u' })
      expect(controller.state.announcement).toBe('Redid hide · 2 transactions · Undo u')
    })

    it('shows no toast and announces the failure when the mutation is rejected', async () => {
      const controller = createEditingController({
        transport: problemTransport('store_error', '7'),
        host: editingHost(testProjection({ revision: '7' })),
      })

      await expect(
        controller.submit({ action: 'transaction.toggle-hidden', input: {}, target }),
      ).resolves.toBe(false)

      expect(notices()).toEqual({ banners: [], status: '' })
      expect(controller.state.announcement).toBe('The profile could not save that action.')
    })

    it('keeps the commit announcement and shows no toast', async () => {
      const response = mutationResponse('8', 'mfsel1.x' as SelectionValue)
      const controller = createEditingController({
        transport: mutationTransport(response),
        host: editingHost(
          testProjection({ revision: '7', canonical_query: response.canonical_query }),
        ),
      })

      await expect(controller.commit(7n)).resolves.toBe(true)

      expect(notices()).toEqual({ banners: [], status: '' })
      expect(controller.state.announcement).toContain('Changes saved in Moneyflow.')
    })

    it('keeps the announcement and shows no toast for a staged rename', async () => {
      const rename: OperationSummary = { ...hide, kind: 'rename', label: 'Rename merchant' }
      const controller = await respondWith(rename, (c) =>
        c.submit({ action: 'transaction.edit-merchant', input: { scope: 'selection' }, target }),
      )

      expect(notices()).toEqual({ banners: [], status: '' })
      expect(controller.state.announcement).toBe('Change saved as pending. Selection cleared.')
    })

    it('keeps an audit warning in the announcement beside a toast', async () => {
      const response = { ...mutationResponse('8', 'mfsel1.x' as SelectionValue), operation: hide }
      const warning = 'The audit log could not record completion.'
      response.projection.warnings = [{ code: 'audit_completion_failed', detail: warning }]
      const controller = createEditingController({
        transport: mutationTransport(response),
        host: editingHost(
          testProjection({ revision: '7', canonical_query: response.canonical_query }),
        ),
      })

      await controller.submit({ action: 'transaction.toggle-hidden', input: {}, target })

      expect(controller.state.announcement).toBe(`Hidden 2 transactions · Undo u ${warning}`)
      // The server repeats the warning until the next commit, so only a commit shows it.
      expect(notices()).toEqual({ banners: [], status: 'Hidden 2 transactions · Undo u' })
    })
  })

  it('refuses every edit made while another is on its way, says why, and sends nothing', async () => {
    let release!: (response: Response) => void
    const response = new Promise<Response>((resolve) => (release = resolve))
    const request = vi.fn<MutationFetch['request']>(async () => await response)
    const controller = createEditingController({
      transport: { request },
      host: editingHost(testProjection({ revision: '7', canonical_query: 'v=1&group=merchant' })),
    })

    const first = controller.submit({ action: 'transaction.edit-merchant', input: {} })
    for (const edit of [
      () => controller.undo(),
      () => controller.redo(),
      () => controller.submit({ action: 'category.manage', input: {} }),
      () => controller.commit(7n),
    ]) {
      await expect(edit()).resolves.toBe(false)
      expect(controller.state.announcement).toBe(
        'Wait for the previous edit to finish, then try again.',
      )
    }
    expect(request).toHaveBeenCalledTimes(1)

    release(jsonResponse(mutationResponse('8', 'mfsel1.example' as SelectionValue)))
    await expect(first).resolves.toBe(true)
    expect(controller.state.announcement).toMatch(/^Change saved as pending\./)
  })

  describe('editing keys', () => {
    const WAIT = 'Wait for the previous edit to finish, then try again.'
    const saved = () => jsonResponse(mutationResponse('8', 'mfsel1.example' as SelectionValue))

    /** A controller whose requests wait for `release`, which answers the oldest one waiting. */
    function heldRequests() {
      const waiting: ((response: Response) => void)[] = []
      const request = vi.fn<MutationFetch['request']>(
        () => new Promise<Response>((resolve) => waiting.push(resolve)),
      )
      const controller = createEditingController({
        transport: { request },
        host: editingHost(testProjection({ revision: '7', canonical_query: 'v=1&group=merchant' })),
      })
      return { controller, request, release: () => waiting.shift()!(saved()) }
    }

    it('refuses a key while a dialog edit is on its way, and takes one after', async () => {
      const { controller, release } = heldRequests()
      const dialog = controller.submit({ action: 'category.manage', input: {} })
      expect(controller.holdForKey()).toBeUndefined()

      release()
      await expect(dialog).resolves.toBe(true)
      expect(controller.holdForKey()).toBeDefined()
    })

    it('refuses dialog edits from a key press until the key’s own edit has settled', async () => {
      const { controller, request, release } = heldRequests()
      const turn = controller.holdForKey()!
      const dialogEdits = [
        () => controller.submit({ action: 'category.manage', input: {} }),
        () => controller.commit(7n),
      ]
      for (const edit of dialogEdits) {
        await expect(edit()).resolves.toBe(false)
        expect(controller.state.announcement).toBe(WAIT)
      }
      expect(request).not.toHaveBeenCalled()

      const keyed = turn.run(() => controller.undo())
      expect(request).toHaveBeenCalledOnce()
      await expect(dialogEdits[0]!()).resolves.toBe(false)
      release()
      await keyed
      expect(controller.state.announcement).not.toBe(WAIT)

      const dialog = dialogEdits[0]!()
      release()
      await expect(dialog).resolves.toBe(true)
      expect(request).toHaveBeenCalledTimes(2)
    })

    it('lets a key wait behind the key whose edit is on its way, so U follows u', async () => {
      const { controller, request, release } = heldRequests()
      const undoing = controller.holdForKey()!.run(() => controller.undo())
      const redo = controller.holdForKey()
      expect(redo).toBeDefined()

      release()
      await undoing
      const redoing = redo!.run(() => controller.redo())
      release()
      await redoing
      expect(request.mock.calls.map(([path]) => path)).toEqual(['api/v1/undo', 'api/v1/redo'])
    })

    it('gives up the turn of a key whose work failed', async () => {
      const { controller, release } = heldRequests()
      const failure = new Error('key failed')
      await expect(
        controller.holdForKey()!.run(() => {
          throw failure
        }),
      ).rejects.toBe(failure)

      const dialog = controller.submit({ action: 'category.manage', input: {} })
      release()
      await expect(dialog).resolves.toBe(true)
    })
  })

  it('takes the next edit once a failed one has finished', async () => {
    const request = vi
      .fn<MutationFetch['request']>()
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockImplementationOnce(async () =>
        jsonResponse(mutationResponse('8', 'mfsel1.example' as SelectionValue)),
      )
    const controller = createEditingController({
      transport: { request },
      host: editingHost(testProjection({ revision: '7', canonical_query: 'v=1&group=merchant' })),
    })

    await expect(controller.undo()).resolves.toBe(false)
    expect(controller.state.announcement).toBe('The profile request failed.')
    await expect(controller.undo()).resolves.toBe(true)
    expect(request).toHaveBeenCalledTimes(2)
  })

  it('rereads a view that moved on during the request instead of reinstalling the old query', async () => {
    let release!: (response: Response) => void
    const response = new Promise<Response>((resolve) => (release = resolve))
    const host = editingHost(
      testProjection({ revision: '7', canonical_query: 'v=1&group=merchant' }),
    )
    const accept = vi.spyOn(host, 'accept')
    const controller = createEditingController({
      transport: { request: vi.fn(async () => await response) },
      host,
    })

    const staged = controller.submit({ action: 'transaction.toggle-hidden', input: {} })
    host.install(testProjection({ revision: '7', canonical_query: 'v=1&period=2026-09' }))
    release(jsonResponse(mutationResponse('8', 'mfsel1.x' as SelectionValue)))

    await expect(staged).resolves.toBe(true)
    expect(accept).not.toHaveBeenCalled()
    // The selection did not change with the view, so the reread takes the response's.
    expect(chosenSelection(host)).toBe('mfsel1.x')
    expect(host.current()?.canonical_query).toBe('v=1&period=2026-09')
    expect(controller.state.revision).toBe(8n)
  })

  it('installs the parent view undo answers with when the drilled target is gone', async () => {
    const drilled = 'v=1&group=merchant&drill=x'
    const host = editingHost(testProjection({ revision: '7', canonical_query: drilled }))
    const accept = vi.spyOn(host, 'accept')
    // Undo removed the drilled target, so the server answers for its parent view.
    const transport = mutationTransport(mutationResponse('8', 'mfsel1.x' as SelectionValue))
    const controller = createEditingController({ transport, host })

    await expect(controller.undo()).resolves.toBe(true)

    const [path, rawBody] = vi.mocked(transport.request).mock.calls[0]!
    expect(path).toBe('api/v1/undo')
    expect(JSON.parse(rawBody)).toMatchObject({ query: drilled })
    expect(accept).toHaveBeenCalledOnce()
    expect(host.current()?.canonical_query).toBe('v=1&group=merchant')
    expect(host.refresh).not.toHaveBeenCalled()
    expect(controller.state.phase).toBe('idle')
    expect(controller.state.revision).toBe(8n)
  })

  it('keeps a selection made while the request ran when rereading a moved-on view', async () => {
    let release!: (response: Response) => void
    const response = new Promise<Response>((resolve) => (release = resolve))
    const host = editingHost(
      testProjection({
        revision: '7',
        canonical_query: 'v=1&group=merchant',
        selection: 'mfsel1.sent' as SelectionValue,
      }),
    )
    const controller = createEditingController({
      transport: { request: vi.fn(async () => await response) },
      host,
    })

    const staged = controller.submit({ action: 'transaction.toggle-hidden', input: {} })
    host.install(
      testProjection({
        revision: '7',
        canonical_query: 'v=1&period=2026-09',
        selection: 'mfsel1.newer' as SelectionValue,
      }),
    )
    release(jsonResponse(mutationResponse('8', 'mfsel1.cleared' as SelectionValue)))

    await expect(staged).resolves.toBe(true)
    expect(chosenSelection(host)).toBeUndefined()
  })

  it('keeps the newer revision a reread of a moved-on view installed', async () => {
    const host = editingHost(
      testProjection({ revision: '7', canonical_query: 'v=1&group=merchant' }),
    )
    const newer = testProjection({
      revision: '9',
      canonical_query: 'v=1&period=2026-09',
      pending: { active_operations: 2, inactive_operations: 0, affected_transactions: 3 },
    })
    // As the shell does, the reread installs the view and syncs the editing controller with it.
    host.refresh = vi.fn(async () => {
      host.install(newer)
      controller.sync(newer)
      return newer
    })
    const transport: MutationFetch = {
      request: vi.fn(async () => {
        host.install(testProjection({ revision: '7', canonical_query: 'v=1&period=2026-09' }))
        return jsonResponse(mutationResponse('8', 'mfsel1.x' as SelectionValue))
      }),
    }
    const controller = createEditingController({ transport, host })

    await expect(
      controller.submit({ action: 'transaction.toggle-hidden', input: {} }),
    ).resolves.toBe(true)
    expect(controller.state.revision).toBe(9n)
    expect(controller.state.pending.active_operations).toBe(2)
  })

  it('reports a staged change whose moved-on view could not be reread', async () => {
    const host = editingHost(
      testProjection({ revision: '7', canonical_query: 'v=1&group=merchant' }),
    )
    host.refresh = vi.fn(async () => {
      throw new Error('offline')
    })
    const transport: MutationFetch = {
      request: vi.fn(async () => {
        host.install(testProjection({ revision: '7', canonical_query: 'v=1&period=2026-09' }))
        return jsonResponse(mutationResponse('8', 'mfsel1.x' as SelectionValue))
      }),
    }
    const controller = createEditingController({ transport, host })

    await expect(
      controller.submit({ action: 'transaction.toggle-hidden', input: {} }),
    ).resolves.toBe(true)
    expect(controller.state.phase).toBe('idle')
    expect(controller.state.announcement).toContain('The current view could not be refreshed.')
    expect(notices().banners).toEqual([])
    expect(notices().status).toContain('The current view could not be refreshed.')
  })

  it('rejects a mutation when the displayed projection and editing revision differ', async () => {
    const host = editingHost(testProjection({ revision: '3' }))
    const transport = mutationTransport(mutationResponse('4', 'mfsel1.example' as SelectionValue))
    const controller = createEditingController({ transport, host })
    host.install(testProjection({ revision: '4' }))

    await expect(controller.undo()).resolves.toBe(false)

    expect(transport.request).not.toHaveBeenCalled()
    expect(controller.state.phase).toBe('conflict')
    expect(controller.state.announcement).toContain('displayed profile revision changed')
  })

  it('refreshes once on revision conflict and never replays the mutation', async () => {
    const host = editingHost(testProjection({ revision: '3' }))
    host.refresh = vi.fn(async () => {
      host.install(testProjection({ revision: '4' }))
      return host.current()
    })
    const transport = problemTransport('revision_conflict', '4')
    const controller = createEditingController({ transport, host })

    await expect(controller.redo()).resolves.toBe(false)

    expect(transport.request).toHaveBeenCalledTimes(1)
    expect(chosenSelection(host)).toBeUndefined()
    expect(controller.state.phase).toBe('conflict')
    expect(controller.state.revision).toBe(4n)
    expect(controller.state.announcement).toContain('changed in another session')
  })

  it('installs a refreshed stale selection and requires explicit reinvocation', async () => {
    const refreshed = 'mfsel1.refreshed' as SelectionValue
    const host = editingHost(testProjection({ revision: '5' }))
    // As the view controller does, a reread without a chosen selection keeps the one shown.
    host.refresh = vi.fn(async (choose) => {
      const shown = host.current()!
      host.install(testProjection({ revision: '5', selection: choose(shown) ?? shown.selection }))
      return host.current()
    })
    const transport: MutationFetch = {
      request: vi.fn(async () =>
        problemResponse('selection_stale', '5', {
          kind: 'refreshed',
          value: refreshed,
        }),
      ),
    }
    const controller = createEditingController({ transport, host })

    await expect(controller.undo()).resolves.toBe(false)

    expect(transport.request).toHaveBeenCalledTimes(1)
    expect(host.current()?.selection).toBe(refreshed)
    expect(controller.state.phase).toBe('idle')
    expect(controller.state.announcement).toContain('Selection refreshed')
  })

  it.each(['store_busy', 'store_error', 'invalid_operation', 'invalid_origin', 'token_expired'])(
    'announces %s without controller-level replay',
    async (code) => {
      const transport = problemTransport(code, '2')
      const controller = createEditingController({
        transport,
        host: editingHost(testProjection({ revision: '2' })),
      })

      await expect(controller.undo()).resolves.toBe(false)

      expect(transport.request).toHaveBeenCalledTimes(1)
      expect(controller.state.phase).toBe('failed')
      expect(controller.state.announcement).not.toContain('private')
    },
  )

  it.each([
    ['taxonomy_label_collision', 'That label already exists; use merge instead.'],
    ['provider_write_batch', 'That action is not available for the current target.'],
    [undefined, 'That action is not available for the current target.'],
  ] as const)('announces an invalid operation with reason %s', async (reason, announcement) => {
    const problem = {
      type: 'about:blank',
      title: 'Unprocessable Entity',
      status: 422,
      detail: 'private server detail',
      code: 'invalid_operation',
      current_revision: '2',
      ...(reason === undefined ? {} : { reason }),
    }
    const transport: MutationFetch = {
      request: vi.fn(async () =>
        Response.json(problem, {
          status: 422,
          headers: { 'Content-Type': 'application/problem+json' },
        }),
      ),
    }
    const controller = createEditingController({
      transport,
      host: editingHost(testProjection({ revision: '2' })),
    })

    await expect(controller.undo()).resolves.toBe(false)

    expect(controller.state.phase).toBe('failed')
    expect(controller.state.announcement).toBe(announcement)
  })

  it('keeps the reviewed revision distinct in commit requests', async () => {
    const transport = mutationTransport(mutationResponse('10', 'mfsel1.example' as SelectionValue))
    const controller = createEditingController({
      transport,
      host: editingHost(testProjection({ revision: '9' })),
    })

    await controller.commit(8n)

    const [, rawBody] = vi.mocked(transport.request).mock.calls[0]!
    expect(JSON.parse(rawBody)).toMatchObject({
      expected_revision: '9',
      reviewed_revision: '8',
    })
  })

  it('reports a committed edit and its audit warning while accepting the new projection', async () => {
    const response = mutationResponse('10', 'mfsel1.example' as SelectionValue)
    const warning = 'The audit log could not record completion.'
    response.projection.warnings = [{ code: 'audit_completion_failed', detail: warning }]
    const transport = mutationTransport(response)
    const host = editingHost(
      testProjection({ revision: '9', canonical_query: response.canonical_query }),
    )
    const controller = createEditingController({ transport, host })

    await expect(controller.commit(9n)).resolves.toBe(true)

    expect(host.current()?.revision).toBe('10')
    expect(controller.state.phase).toBe('idle')
    expect(controller.state.announcement).toContain('Changes saved in Moneyflow.')
    expect(controller.state.announcement).toContain(warning)
    expect(notices()).toEqual({ banners: [], status: warning })
  })

  it('loads bounded editor choices at the exact current revision', async () => {
    const transport = mutationTransport({
      version: '1',
      revision: '6',
      merchants: [],
      categories: [],
      groups: [],
    })
    const controller = createEditingController({
      transport,
      host: editingHost(testProjection({ revision: '6' })),
    })
    await controller.catalog()
    const [path, rawBody] = vi.mocked(transport.request).mock.calls[0]!
    expect(path).toBe('api/v1/editor-catalog')
    expect(JSON.parse(rawBody)).toEqual({ version: '1', expected_revision: '6' })
  })

  it('requests merchant preview with the full selection and view, independent of loaded rows', async () => {
    const projection = testProjection({
      revision: '6',
      selection: 'mfsel1.selected' as SelectionValue,
    })
    const transport = mutationTransport({
      version: '1',
      revision: '6',
      affected_transactions: 15,
      transactions: [],
    })
    const controller = createEditingController({ transport, host: editingHost(projection) })
    const preview = await controller.previewMerchant(
      { kind: 'aggregate', identity: 'row-a' },
      'transactions',
    )
    expect(preview.affected_transactions).toBe(15)
    const [path, rawBody] = vi.mocked(transport.request).mock.calls[0]!
    expect(path).toBe('api/v1/merchant-preview')
    expect(JSON.parse(rawBody)).toMatchObject({
      expected_revision: '6',
      query: projection.canonical_query,
      selection: 'mfsel1.selected',
      target: { kind: 'aggregate', identity: 'row-a' },
      input: { scope: 'transactions' },
    })
  })

  it('neither rereads nor retries a preview refused after it stopped', async () => {
    const answered = Promise.withResolvers<void>()
    const transport = {
      request: vi.fn(async () => {
        await answered.promise
        return problemResponse('selection_stale', '6')
      }),
    }
    const host = editingHost(testProjection({ revision: '6' }))
    const controller = createEditingController({ transport, host })
    const preview = controller.previewMerchant(
      { kind: 'transaction', identity: 'row-0' },
      'transactions',
    )

    controller.stop()
    answered.resolve()

    await expect(preview).rejects.toBeInstanceOf(MoneyflowProblem)
    expect({
      requests: transport.request.mock.calls.length,
      rereads: vi.mocked(host.refresh).mock.calls.length,
      announcement: controller.state.announcement,
    }).toEqual({ requests: 1, rereads: 0, announcement: '' })
  })
})

function editingHost(initial: ReturnType<typeof testProjection>): EditingProjectionHost & {
  accept(next: ViewProjection): void
  install(next: ReturnType<typeof testProjection>): void
} {
  let projection = initial
  const host = {
    current: () => projection,
    /** Shows an edit's view; every claim the fake hands out calls it. */
    accept: (next: ViewProjection) => {
      projection = next
    },
    claim: () => (next: ViewProjection) => {
      host.accept(next)
      return true
    },
    settled: async () => undefined,
    refresh: vi.fn(async () => projection),
    install: (next: ViewProjection) => {
      projection = next
    },
  }
  return host
}

/** The selection the edit's one reread chose from the view the fake shows. */
function chosenSelection(host: ReturnType<typeof editingHost>): SelectionValue | undefined {
  expect(host.refresh).toHaveBeenCalledOnce()
  const [choose] = vi.mocked(host.refresh).mock.calls[0]!
  return choose(host.current()!)
}

function mutationTransport(body: unknown): MutationFetch {
  return { request: vi.fn(async () => jsonResponse(body)) }
}

function problemTransport(code: string, revision: string): MutationFetch {
  return { request: vi.fn(async () => problemResponse(code, revision)) }
}

function mutationResponse(
  revision: string,
  selection: SelectionValue,
  pending = { active_operations: 0, inactive_operations: 0, affected_transactions: 0 },
) {
  return {
    version: '1',
    revision,
    canonical_query: 'v=1&group=merchant',
    projection: testProjection({
      revision,
      canonical_query: 'v=1&group=merchant',
      selection,
      pending,
    }),
    pending,
    selection: { kind: 'cleared', value: selection },
  }
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

function problemResponse(
  code: string,
  revision: string,
  selection?: { kind: string; value: string },
): Response {
  const problem = new MoneyflowProblem({
    type: 'about:blank',
    title: 'Request rejected',
    status: 409,
    detail: 'private server detail',
    code,
    current_revision: revision,
    ...(selection === undefined ? {} : { selection }),
  }).problem
  return new Response(JSON.stringify(problem), {
    status: problem.status,
    headers: { 'Content-Type': 'application/problem+json' },
  })
}
