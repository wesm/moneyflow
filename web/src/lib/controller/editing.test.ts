import { beforeEach, describe, expect, it, vi } from 'vitest'

import { MoneyflowProblem, type MutationFetch, type SelectionValue } from '../api/client'
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
    expect(host.refresh).toHaveBeenCalledTimes(1)
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
    expect(host.refresh).toHaveBeenCalledWith(refreshed)
    expect(controller.state.phase).toBe('idle')
    expect(controller.state.announcement).toContain('Selection refreshed')
  })

  it.each(['store_busy', 'store_error', 'invalid_operation', 'token_expired'])(
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
