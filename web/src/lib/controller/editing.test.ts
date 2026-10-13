import { dismissFlash, getFlashes } from '@kenn-io/kit-ui'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  MoneyflowProblem,
  type MutationFetch,
  type OperationSummary,
  type SelectionValue,
} from '../api/client'
import { testProjection } from '../../test/projection'
import { createEditingController, type EditingProjectionHost } from './editing'

describe('browser editing controller', () => {
  beforeEach(() => history.replaceState({ safe: true }, '', '/moneyflow/?v=1&group=merchant'))
  afterEach(() => dismissFlash())

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
    afterEach(() => dismissFlash())

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

      expect(getFlashes().map((flash) => flash.message)).toEqual(['Hidden 2 transactions · Undo u'])
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

      expect(getFlashes().map((flash) => flash.message)).toEqual([
        'Undid delete · 1 transaction · Redo U',
      ])
      expect(controller.state.announcement).toBe('Undid delete · 1 transaction · Redo U')
    })

    it('toasts and announces a redo', async () => {
      const controller = await respondWith(hide, (c) => c.redo())

      expect(getFlashes().map((flash) => flash.message)).toEqual([
        'Redid hide · 2 transactions · Undo u',
      ])
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

      expect(getFlashes()).toEqual([])
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

      expect(getFlashes()).toEqual([])
      expect(controller.state.announcement).toContain('Changes saved in Moneyflow.')
    })

    it('keeps the announcement and shows no toast for a staged rename', async () => {
      const rename: OperationSummary = { ...hide, kind: 'rename', label: 'Rename merchant' }
      const controller = await respondWith(rename, (c) =>
        c.submit({ action: 'transaction.edit-merchant', input: { scope: 'selection' }, target }),
      )

      expect(getFlashes()).toEqual([])
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
      expect(getFlashes().map(({ message }) => message)).toEqual(['Hidden 2 transactions · Undo u'])
    })
  })

  it('suppresses duplicate submissions while one request is in flight', async () => {
    let release!: (response: Response) => void
    const response = new Promise<Response>((resolve) => (release = resolve))
    const transport: MutationFetch = { request: vi.fn(async () => await response) }
    const controller = createEditingController({
      transport,
      host: editingHost(testProjection({ canonical_query: 'v=1&group=merchant' })),
    })

    const first = controller.undo()
    await expect(controller.undo()).resolves.toBe(false)
    expect(transport.request).toHaveBeenCalledTimes(1)
    release(jsonResponse(mutationResponse('1', 'mfsel1.example' as SelectionValue)))
    await expect(first).resolves.toBe(true)
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
    expect(host.refresh).toHaveBeenCalledWith('mfsel1.x', true)
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
    expect(host.refresh).toHaveBeenCalledWith(undefined, true)
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
    expect(getFlashes().map(({ message, tone }) => ({ message, tone }))).toContainEqual({
      message: 'The current view could not be refreshed.',
      tone: 'warning',
    })
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
    expect(host.refresh).toHaveBeenCalledWith(undefined, true)
    expect(controller.state.phase).toBe('conflict')
    expect(controller.state.revision).toBe(4n)
    expect(controller.state.announcement).toContain('changed in another session')
  })

  it('installs a refreshed stale selection and requires explicit reinvocation', async () => {
    const refreshed = 'mfsel1.refreshed' as SelectionValue
    const host = editingHost(testProjection({ revision: '5' }))
    host.refresh = vi.fn(async (selection) => {
      host.install(testProjection({ revision: '5', selection: selection ?? refreshed }))
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
    expect(host.refresh).toHaveBeenCalledWith(refreshed, true)
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
    expect(getFlashes().map(({ message, tone }) => ({ message, tone }))).toEqual([
      { message: warning, tone: 'warning' },
    ])
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
})

function editingHost(initial: ReturnType<typeof testProjection>): EditingProjectionHost & {
  install(next: ReturnType<typeof testProjection>): void
} {
  let projection = initial
  return {
    current: () => projection,
    accept: (next) => {
      projection = next
    },
    refresh: vi.fn(async () => projection),
    install: (next) => {
      projection = next
    },
  }
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
