import { describe, expect, it, vi } from 'vitest'

import type { ProviderStatus, ViewProjection } from '../api/client'
import { createProviderController } from './provider'
import { testProjection } from '../../test/projection'

describe('provider controller', () => {
  it.each(['recent', 'full'] as const)(
    'starts the %s refresh the server reports as due',
    async (refresh_due) => {
      const current = testProjection({ capabilities: [capability(true)] })
      const transport = transportStub({
        status: providerStatus({ provider_kind: 'monarch', refresh_due }),
        mutationResponse: Response.json(refreshResponse(current, 'preserved')),
      })
      const controller = createProviderController({ transport, host: hostStub(current) })
      controller.sync(current)

      await controller.poll()

      expect(transport.mutations.request).toHaveBeenCalledOnce()
      expect(JSON.parse(vi.mocked(transport.mutations.request).mock.calls[0]![1]).manual).toBe(
        false,
      )
    },
  )

  it('waits when no refresh is due and refreshes on request', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      status: providerStatus({ provider_kind: 'monarch', last_success: '2026-08-01T12:00:00Z' }),
      mutationResponse: Response.json(refreshResponse(current, 'preserved')),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await controller.poll()
    expect(transport.mutations.request).not.toHaveBeenCalled()
    await controller.refresh()
    expect(transport.mutations.request).toHaveBeenCalledOnce()
    expect(JSON.parse(vi.mocked(transport.mutations.request).mock.calls[0]![1]).manual).toBe(true)
  })

  it('announces a warning the refresh reports', async () => {
    const warning = 'Refresh saved, but the full refresh was not recorded.'
    const current = testProjection({ capabilities: [capability(true)] })
    const response = refreshResponse(current, 'preserved')
    response.status = { ...response.status, warning }
    const transport = transportStub({
      status: providerStatus({ provider_kind: 'monarch', warning }),
      mutationResponse: Response.json(response),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await controller.refresh()
    expect(controller.state.announcement).toBe(`Provider refresh complete. ${warning}`)
  })

  it('does not dispatch an unavailable refresh and announces the server reason', async () => {
    const transport = transportStub()
    const controller = createProviderController({ transport, host: hostStub() })
    controller.sync(
      testProjection({
        capabilities: [capability(false, 'Connect a provider before refreshing.')],
      }),
    )

    await expect(controller.refresh()).resolves.toBe(false)
    expect(transport.mutations.request).not.toHaveBeenCalled()
    expect(controller.state.announcement).toBe('Connect a provider before refreshing.')
  })

  it('accepts one authoritative refresh without changing analytical state', async () => {
    const current = testProjection({
      canonical_query: 'v=1&group=category',
      revision: '1',
      capabilities: [capability(true)],
    })
    const next = testProjection({
      canonical_query: current.canonical_query,
      revision: '2',
      selection: 'mfsel1.next' as ViewProjection['selection'],
    })
    const host = hostStub(current)
    const transport = transportStub({
      mutationResponse: Response.json(refreshResponse(next, 'preserved')),
    })
    const controller = createProviderController({ transport, host })
    controller.sync(current)

    await expect(controller.refresh()).resolves.toBe(true)
    expect(host.accept).toHaveBeenCalledWith(next)
    expect(controller.state.phase).toBe('idle')
    expect(controller.state.announcement).toContain('complete')
    expect(JSON.parse(vi.mocked(transport.mutations.request).mock.calls[0]![1])).toMatchObject({
      manual: true,
      query: current.canonical_query,
      selection: current.selection,
    })
  })

  it('clears exact selection and parks reconnect without replay', async () => {
    const current = testProjection({ selection_count: 2, capabilities: [capability(true)] })
    const cleared = testProjection({
      revision: '2',
      selection: 'mfsel1.empty' as ViewProjection['selection'],
      selection_count: 0,
    })
    const host = hostStub(current)
    const transport = transportStub({
      mutationResponse: Response.json(refreshResponse(cleared, 'cleared')),
    })
    const controller = createProviderController({ transport, host })
    controller.sync(current)
    await controller.refresh()
    expect(controller.state.announcement).toContain('Selection cleared')

    vi.mocked(transport.mutations.request).mockResolvedValueOnce(
      problemResponse('provider_reconnect_required', 'Reconnect through the command line.'),
    )
    await expect(controller.refresh()).resolves.toBe(false)
    expect(controller.state.phase).toBe('reconnect')
    expect(transport.mutations.request).toHaveBeenCalledTimes(2)

    vi.mocked(transport.status).mockResolvedValueOnce(
      providerStatus({ last_success: new Date().toISOString() }),
    )
    await controller.poll()
    expect(controller.state.phase).toBe('idle')
    expect(controller.state.announcement).toContain('restored')
    expect(transport.mutations.request).toHaveBeenCalledTimes(2)
  })

  it('requires explicit confirmation and refetches status after an invalid token', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const pending = providerStatus({
      code: 'provider_deletion_confirmation_required',
      confirmation_token: 'opaque-confirmation',
      summary: { ...emptySummary(), removed_transactions: 5 },
    })
    const transport = transportStub({
      mutationResponse: problemResponse(
        'provider_deletion_confirmation_required',
        'Confirm provider removals.',
        pending,
      ),
      status: providerStatus(),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await expect(controller.refresh()).resolves.toBe(false)
    expect(controller.state.phase).toBe('confirmation')
    expect(controller.state.status?.summary.removed_transactions).toBe(5)

    vi.mocked(transport.mutations.request).mockResolvedValueOnce(
      problemResponse('provider_confirmation_invalid', 'Confirmation expired.'),
    )
    await expect(controller.confirm()).resolves.toBe(false)
    expect(transport.status).toHaveBeenCalledTimes(1)
    expect(transport.mutations.request).toHaveBeenCalledTimes(2)
    expect(controller.state.phase).toBe('failed')
  })

  it('keeps the failure and the status returned with a refresh error', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const failed = providerStatus({
      provider_kind: 'monarch',
      code: 'provider_unavailable',
      code_detail: 'The provider is temporarily unavailable.',
      last_attempt: '2026-10-11T12:00:00Z',
    })
    const transport = transportStub({
      mutationResponse: problemResponse(
        'provider_unavailable',
        'The provider is temporarily unavailable.',
        failed,
      ),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await expect(controller.refresh()).resolves.toBe(false)

    expect(controller.state.phase).toBe('failed')
    expect(controller.state.announcement).toBe('The provider is temporarily unavailable.')
    expect(controller.state.status).toEqual(failed)
  })

  it('reports a failed last attempt from a poll', async () => {
    const transport = transportStub({
      status: providerStatus({
        provider_kind: 'monarch',
        code: 'provider_unavailable',
        code_detail: 'The provider is temporarily unavailable.',
        last_success: '2026-10-03T12:00:00Z',
      }),
    })
    const controller = createProviderController({ transport, host: hostStub() })

    await controller.poll()

    expect(controller.state.phase).toBe('failed')
    expect(controller.state.announcement).toBe(
      'Last refresh failed: The provider is temporarily unavailable.',
    )
    expect(transport.mutations.request).not.toHaveBeenCalled()
  })

  it('retries a failed refresh once the server reports a refresh as due', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      status: providerStatus({
        provider_kind: 'monarch',
        code: 'provider_unavailable',
        code_detail: 'The provider is temporarily unavailable.',
        last_success: '2026-10-03T12:00:00Z',
        refresh_due: 'recent',
      }),
      mutationResponse: Response.json(refreshResponse(current, 'preserved')),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await controller.poll()

    expect(transport.mutations.request).toHaveBeenCalledOnce()
    expect(JSON.parse(vi.mocked(transport.mutations.request).mock.calls[0]![1]).manual).toBe(
      false,
    )
    expect(controller.state.phase).toBe('idle')
  })

  it('names the Monarch refresh kind it starts', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    let finish: (response: Response) => void = () => undefined
    const transport = transportStub({
      status: providerStatus({ provider_kind: 'monarch', refresh_due: 'recent' }),
    })
    vi.mocked(transport.mutations.request).mockImplementation(
      () => new Promise<Response>((resolve) => (finish = resolve)),
    )
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    const scheduled = controller.poll()
    await vi.waitFor(() => expect(controller.state.kind).toBe('recent'))
    finish(
      Response.json({
        ...refreshResponse(current, 'preserved'),
        status: providerStatus({ provider_kind: 'monarch' }),
      }),
    )
    await scheduled
    expect(controller.state.kind).toBeUndefined()

    const manual = controller.refresh()
    expect(controller.state.kind).toBe('full')
    finish(Response.json(refreshResponse(current, 'preserved')))
    await manual
  })

  it('polls status every second only while a refresh runs', async () => {
    vi.useFakeTimers()
    try {
      const current = testProjection({ capabilities: [capability(true)] })
      let finish: (response: Response) => void = () => undefined
      const transport = transportStub()
      vi.mocked(transport.mutations.request).mockImplementation(
        () => new Promise<Response>((resolve) => (finish = resolve)),
      )
      const controller = createProviderController({ transport, host: hostStub(current) })
      controller.sync(current)

      const refreshing = controller.refresh()
      await vi.advanceTimersByTimeAsync(2_000)
      expect(transport.status).toHaveBeenCalledTimes(2)

      finish(Response.json(refreshResponse(current, 'preserved')))
      await refreshing
      await vi.advanceTimersByTimeAsync(5_000)
      expect(transport.status).toHaveBeenCalledTimes(2)
      expect(controller.state.phase).toBe('idle')
    } finally {
      vi.useRealTimers()
    }
  })

  it('reports another process progress without starting a competing refresh', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      status: providerStatus({
        code: 'provider_refresh_in_progress',
        progress: { fetched: 20, total: 40 },
      }),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await controller.poll()

    expect(controller.state.phase).toBe('refreshing')
    expect(controller.state.status?.progress).toEqual({ fetched: 20, total: 40 })
    expect(transport.mutations.request).not.toHaveBeenCalled()
  })
})

function capability(available: boolean, reason?: string) {
  return {
    id: 'provider.refresh',
    key_display: 'r',
    description: 'Refresh provider data',
    category: 'System',
    available,
    ...(reason === undefined ? {} : { reason }),
  }
}

function hostStub(current = testProjection()) {
  return {
    current: vi.fn(() => current),
    accept: vi.fn(),
  }
}

function transportStub(options: { mutationResponse?: Response; status?: ProviderStatus } = {}) {
  return {
    mutations: {
      request: vi.fn(async (path: string, body: string, signal?: AbortSignal) => {
        void path
        void body
        void signal
        return options.mutationResponse ?? Response.json({})
      }),
    },
    status: vi.fn(async () => options.status ?? providerStatus()),
  }
}

function providerStatus(overrides: Partial<ProviderStatus> = {}): ProviderStatus {
  return {
    version: '1',
    revision: '1',
    generation: '1',
    refresh_due: 'none',
    progress: { fetched: 0, total: 0 },
    summary: emptySummary(),
    capability: capability(true),
    ...overrides,
  }
}

function emptySummary() {
  return {
    imported_accounts: 0,
    imported_merchants: 0,
    imported_groups: 0,
    imported_categories: 0,
    imported_transactions: 0,
    removed_transactions: 0,
    removed_operations: 0,
    removed_targets: 0,
    retained_operations: 0,
    rebased_hide_targets: 0,
    discarded_redo_operations: 0,
  }
}

function refreshResponse(projection: ViewProjection, kind: string) {
  return {
    version: '1',
    revision: projection.revision,
    generation: '2',
    status: providerStatus({ revision: projection.revision, generation: '2' }),
    projection,
    selection: { kind, value: projection.selection },
  }
}

function problemResponse(code: string, detail: string, status?: ProviderStatus): Response {
  const problem = {
    type: 'about:blank',
    title: 'Conflict',
    status: 409,
    detail,
    code,
    ...(status === undefined ? {} : { provider: status }),
  }
  return Response.json(problem, {
    status: 409,
    headers: { 'Content-Type': 'application/problem+json' },
  })
}
