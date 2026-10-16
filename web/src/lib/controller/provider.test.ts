import { describe, expect, it, vi } from 'vitest'

import type { ProviderStatus, ViewProjection } from '../api/client'
import {
  createProviderController,
  REFRESH_CANCELED,
  REFRESH_NOT_CONFIRMED,
  REFRESH_STOPPED,
} from './provider'
import { staleNotice } from './refresh-notice'
import { notices } from '../../test/notices'
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
    expect(controller.state.announcement).toBe(
      `Refresh complete: 0 imported, 0 removed. ${warning}`,
    )
    expect(notices()).toEqual({
      banners: [],
      status: `Refresh complete: 0 imported, 0 removed. ${warning}`,
    })
    expect(controller.state.dialog).toBeUndefined()
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

  it('closes a completed refresh while the queued view reread loads, then reports its failure', async () => {
    const sent = testProjection({
      revision: '1',
      canonical_query: 'v=1',
      capabilities: [capability(true)],
    })
    const host = hostStub(sent)
    const viewChange = Promise.withResolvers<void>()
    host.serially = (work) => viewChange.promise.then(work)
    const reread = Promise.withResolvers<undefined>()
    host.reload.mockReturnValueOnce(reread.promise)
    const transport = transportStub()
    vi.mocked(transport.mutations.request).mockImplementation(async () => {
      host.current.mockReturnValue(
        testProjection({ revision: '1', canonical_query: 'group=category&v=1' }),
      )
      return Response.json(refreshResponse({ ...sent, revision: '2' }, 'preserved'))
    })
    const controller = createProviderController({ transport, host })
    controller.sync(sent)

    const refresh = controller.refresh()
    await settle()

    expect(controller.state.dialog).toBeUndefined()
    expect(controller.state.phase).toBe('idle')
    expect(staleNotice(controller.state, Date.now())?.loading).toBe(true)
    expect(host.reload).not.toHaveBeenCalled()

    viewChange.resolve()
    await vi.waitFor(() => expect(host.reload).toHaveBeenCalledOnce())
    expect(controller.state.dialog).toBeUndefined()
    expect(staleNotice(controller.state, Date.now())?.loading).toBe(true)
    reread.reject(new Error('offline'))
    await expect(refresh).resolves.toBe(true)

    expect(host.accept).not.toHaveBeenCalled()
    expect(staleNotice(controller.state, Date.now())?.rereadFailed).toBe(true)
    expect(controller.state.announcement).toBe(
      'Refresh complete: 0 imported, 0 removed. The current view could not be reread.',
    )
    expect(notices().status).toBe(
      'Refresh complete: 0 imported, 0 removed. The current view could not be reread.',
    )
  })

  // Installing the refreshed view shows its warnings; the completion notice replaces them, so it
  // must carry them. Editing reports the audit warning itself.
  it('keeps why a refresh cleared the selection in the completion notice', async () => {
    const current = testProjection({ selection_count: 2, capabilities: [capability(true)] })
    const reason = 'The selection changed and was cleared after refresh.'
    const cleared = testProjection({
      revision: '2',
      selection: 'mfsel1.empty' as ViewProjection['selection'],
      selection_count: 0,
      warnings: [
        { code: 'view_notice', detail: reason },
        { code: 'audit_completion_failed', detail: 'The audit log could not be completed.' },
      ],
    })
    const transport = transportStub({
      mutationResponse: Response.json(refreshResponse(cleared, 'cleared')),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await controller.refresh()

    const summary = `Refresh complete: 0 imported, 0 removed. ${reason}`
    expect(notices()).toEqual({ banners: [], status: summary })
    expect(controller.state.announcement).toBe(summary)
  })

  it('clears exact selection and parks reconnect without replay', async () => {
    const current = testProjection({ selection_count: 2, capabilities: [capability(true)] })
    const cleared = testProjection({
      revision: '2',
      selection: 'mfsel1.empty' as ViewProjection['selection'],
      selection_count: 0,
      warnings: [
        { code: 'view_notice', detail: 'The selection changed and was cleared after refresh.' },
      ],
    })
    const host = hostStub(current)
    const transport = transportStub({
      mutationResponse: Response.json(refreshResponse(cleared, 'cleared')),
    })
    const controller = createProviderController({ transport, host })
    controller.sync(current)
    await controller.refresh()
    expect(controller.state.announcement).toContain('cleared after refresh')

    vi.mocked(transport.mutations.request).mockResolvedValueOnce(
      problemResponse('provider_reconnect_required', 'Reconnect through the command line.'),
    )
    await expect(controller.refresh()).resolves.toBe(false)
    expect(controller.state.phase).toBe('reconnect')
    expect(transport.mutations.request).toHaveBeenCalledTimes(2)

    vi.mocked(transport.status).mockResolvedValueOnce(
      providerStatus({ generation: '2', revision: '2', last_success: new Date().toISOString() }),
    )
    await controller.poll()
    expect(controller.state.phase).toBe('idle')
    expect(controller.state.announcement).toContain('restored')
    expect(transport.mutations.request).toHaveBeenCalledTimes(2)
  })

  it.each(['with', 'without'] as const)(
    'offers reconnect after Continue for a reconnect error %s its provider status',
    async (sent) => {
      const current = testProjection({ capabilities: [capability(true)] })
      const reconnect = providerStatus({
        provider_kind: 'monarch',
        code: 'provider_reconnect_required',
        last_success: '2026-10-03T12:00:00Z',
      })
      const transport = transportStub({
        status: providerStatus({
          provider_kind: 'monarch',
          last_success: '2026-10-03T12:00:00Z',
          next_eligible: '2999-01-01T00:00:00Z',
        }),
        mutationResponse: problemResponse(
          'provider_reconnect_required',
          'Reconnect through the command line.',
          sent === 'with' ? reconnect : undefined,
        ),
      })
      const controller = createProviderController({ transport, host: hostStub(current) })
      controller.sync(current)
      await controller.poll()
      await controller.refresh()

      controller.continueStale()

      expect(staleNotice(controller.state, Date.now())?.reconnect).toBe(true)
      expect(controller.state.status?.code).toBe('provider_reconnect_required')
    },
  )

  it.each(['none', 'retry', 'poll'] as const)(
    'shows an expired confirmation before its status read, preserving a later %s',
    async (later) => {
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
      const statusRead = Promise.withResolvers<ProviderStatus>()
      transport.status.mockReturnValueOnce(statusRead.promise)
      const confirmation = controller.confirm()
      await settle()

      expect(controller.state.dialog?.view).toBe('failed')
      expect(controller.state.dialog?.reason).toContain('Confirmation expired.')
      expect(transport.mutations.request).toHaveBeenCalledTimes(2)
      controller.continueStale()
      expect(controller.state.dialog).toBeUndefined()
      if (later === 'retry') {
        transport.mutations.request.mockResolvedValueOnce(
          problemResponse('provider_reconnect_required', 'Reconnect.'),
        )
        await controller.refresh()
        expect(controller.state.dialog?.view).toBe('reconnect')
      } else if (later === 'poll') {
        transport.status.mockResolvedValueOnce(providerStatus({ code: 'provider_unavailable' }))
        await controller.poll()
      }
      const latest = controller.state
      statusRead.resolve(providerStatus())
      await expect(confirmation).resolves.toBe(false)

      if (later === 'none') {
        expect(controller.state.status?.confirmation_token).toBeUndefined()
        expect(controller.state.dialog).toBeUndefined()
      } else {
        expect(controller.state).toEqual(latest)
      }
    },
  )

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
    expect(controller.state.dialog).toMatchObject({
      view: 'failed',
      reason: 'The provider is temporarily unavailable.',
    })
    expect(controller.state.status).toEqual(failed)
  })

  it('keeps a failed refresh in its dialog instead of a passing banner', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      mutationResponse: problemResponse(
        'provider_unavailable',
        'The provider is temporarily unavailable.',
      ),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await controller.refresh()

    expect(controller.state.dialog?.view).toBe('failed')
    expect(notices()).toEqual({ banners: [], status: '' })
  })

  it('points an unexpected server error to the server log', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      mutationResponse: problemResponse(
        'internal_error',
        'The request could not be completed.',
        undefined,
        500,
      ),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await controller.refresh()

    expect(controller.state.phase).toBe('failed')
    expect(controller.state.dialog?.reason).toBe(
      'The request could not be completed. The terminal running moneyflow web shows the cause.',
    )
  })

  it('explains a refresh that never reached the server', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub()
    vi.mocked(transport.mutations.request).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await controller.refresh()

    expect(controller.state.dialog?.reason).toBe(
      'Moneyflow could not reach the server. Check the connection and retry.',
    )
  })

  it('opens the dialog for a background refresh and keeps it open when it fails', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      status: providerStatus({
        provider_kind: 'monarch',
        last_success: '2026-10-02T12:00:00Z',
        refresh_due: 'full',
      }),
      mutationResponse: problemResponse(
        'provider_refresh_timeout',
        'The refresh did not finish within 30 minutes, so Moneyflow stopped it.',
        undefined,
        504,
      ),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await controller.poll()

    expect(controller.state.dialog).toMatchObject({
      view: 'failed',
      reason: 'The refresh did not finish within 30 minutes, so Moneyflow stopped it.',
    })
    await controller.poll()
    expect(controller.state.dialog?.view).toBe('failed')
    expect(transport.mutations.request).toHaveBeenCalledOnce()
  })

  it('continues with stale data under the failure the dialog showed', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      mutationResponse: problemResponse('provider_rate_limited', 'Rate limited.'),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)
    await controller.refresh()

    controller.continueStale()

    expect(controller.state.dialog).toBeUndefined()
    expect(controller.state.staleReason).toBe('Rate limited.')
  })

  it('retries from the failed dialog with a new manual refresh', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      mutationResponse: problemResponse('provider_unavailable', 'Unavailable.'),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)
    await controller.refresh()
    vi.mocked(transport.mutations.request).mockResolvedValueOnce(
      Response.json(refreshResponse(current, 'preserved')),
    )

    await expect(controller.refresh()).resolves.toBe(true)

    expect(controller.state.dialog).toBeUndefined()
    expect(controller.state.staleReason).toBeUndefined()
  })

  it('cancels by aborting the request and waits for the server to stop', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({ status: providerStatus({ refreshing: true }) })
    let signal: AbortSignal | undefined
    vi.mocked(transport.mutations.request).mockImplementation(async (_path, _body, sent) => {
      signal = sent
      return new Promise<Response>((_resolve, reject) =>
        sent?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
      )
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)
    const refreshing = controller.refresh()
    await vi.waitFor(() => expect(signal).toBeDefined())

    controller.cancel()

    expect(signal?.aborted).toBe(true)
    await expect(refreshing).resolves.toBe(false)
    expect(controller.state.dialog?.view).toBe('stopping')

    vi.mocked(transport.status).mockResolvedValue(
      providerStatus({ code: 'provider_unavailable', last_success: '2026-10-03T12:00:00Z' }),
    )
    await controller.poll()
    expect(controller.state.dialog).toBeUndefined()
    expect(controller.state.announcement).toBe(REFRESH_STOPPED)
  })

  it('lets the user leave a stopping dialog whose status checks fail', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      status: providerStatus({ refreshing: true, last_success: '2026-10-03T12:00:00Z' }),
    })
    vi.mocked(transport.mutations.request).mockImplementation(
      async (_path, _body, sent) =>
        new Promise<Response>((_resolve, reject) =>
          sent?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
        ),
    )
    const controller = createProviderController({
      transport,
      host: hostStub(current),
      standalone: true,
    })
    controller.sync(current)
    await controller.poll()
    void controller.refresh()
    controller.cancel()
    vi.mocked(transport.status).mockRejectedValue(new TypeError('Failed to fetch'))
    await controller.poll()
    expect(controller.state.dialog?.view).toBe('stopping')

    controller.continueStale()

    expect(controller.state.dialog).toBeUndefined()
    expect(staleNotice(controller.state, Date.parse('2026-10-13T12:00:00Z'))?.checkFailed).toBe(
      true,
    )
  })

  it('lets the user leave another interface refresh once status checks fail', async () => {
    const transport = transportStub({ status: providerStatus({ refreshing: true }) })
    const controller = createProviderController({ transport, host: hostStub(), standalone: true })
    await controller.poll()
    vi.mocked(transport.status).mockRejectedValue(new TypeError('Failed to fetch'))
    await controller.poll()

    controller.continueStale()

    expect(controller.state.dialog).toBeUndefined()
    expect(staleNotice(controller.state, Date.now())?.checkFailed).toBe(true)
  })

  it('does not restart a due refresh the user canceled before the server took it', async () => {
    let clock = Date.parse('2026-10-13T12:00:00Z')
    const current = testProjection({ capabilities: [capability(true)] })
    const due = providerStatus({ provider_kind: 'monarch', refresh_due: 'recent' })
    const transport = transportStub({ status: due })
    vi.mocked(transport.mutations.request).mockImplementation(
      async (_path, _body, sent) =>
        new Promise<Response>((_resolve, reject) =>
          sent?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
        ),
    )
    const controller = createProviderController({
      transport,
      host: hostStub(current),
      now: () => clock,
    })
    controller.sync(current)

    void controller.poll()
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledOnce())
    controller.cancel()
    await vi.waitFor(() => expect(controller.state.dialog).toBeUndefined())
    await controller.poll()

    expect(transport.mutations.request).toHaveBeenCalledOnce()
    expect(controller.state.dialog).toBeUndefined()
    expect(controller.state.announcement).toBe(REFRESH_STOPPED)

    clock += 5 * 60_000
    void controller.poll()
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledTimes(2))
    controller.cancel()
    await vi.waitFor(() => expect(controller.state.dialog).toBeUndefined())
  })

  it('refreshes on request right after a canceled automatic refresh', async () => {
    const host = hostStub()
    host.current.mockReturnValue(undefined as unknown as ViewProjection)
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      status: providerStatus({ provider_kind: 'monarch', refresh_due: 'recent' }),
      mutationResponse: Response.json(refreshResponse(current, 'preserved')),
    })
    const controller = createProviderController({ transport, host })

    await controller.poll()
    controller.cancel()
    host.current.mockReturnValue(current)
    controller.sync(current)
    expect(controller.state.staleReason).toBe(REFRESH_CANCELED)
    await controller.poll()
    expect(transport.mutations.request).not.toHaveBeenCalled()

    await expect(controller.refresh()).resolves.toBe(true)
    expect(JSON.parse(vi.mocked(transport.mutations.request).mock.calls[0]![1]).manual).toBe(true)
  })

  it('follows a conflicting refresh from the generation the server reported', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const earlier = { ...emptySummary(), imported_transactions: 40 }
    const transport = transportStub({ status: providerStatus({ summary: earlier }) })
    const controller = createProviderController({
      transport,
      host: hostStub(current),
      standalone: true,
    })
    controller.sync(current)
    await controller.poll()
    vi.mocked(transport.mutations.request).mockResolvedValueOnce(
      problemResponse(
        'provider_refresh_in_progress',
        'Another refresh is running.',
        providerStatus({ generation: '2', summary: earlier }),
        503,
      ),
    )
    await controller.refresh()
    expect(controller.state.dialog?.view).toBe('other')

    vi.mocked(transport.status).mockResolvedValue(
      providerStatus({ generation: '2', code: 'provider_unavailable', summary: earlier }),
    )
    await controller.poll()

    expect(controller.state.dialog).toBeUndefined()
    expect(controller.state.announcement).toBe('The other refresh ended.')
    expect(notices()).toEqual({ banners: [], status: '' })
  })

  it('does not attribute saved data to a canceled retry', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const failure = (generation: string) =>
      problemResponse(
        'provider_unavailable',
        'Unavailable.',
        providerStatus({ generation, code: 'provider_unavailable' }),
      )
    const transport = transportStub({ mutationResponse: failure('1') })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)
    await controller.poll()
    await controller.refresh()
    vi.mocked(transport.mutations.request).mockResolvedValueOnce(failure('2'))
    await controller.refresh()
    vi.mocked(transport.status).mockResolvedValue(
      providerStatus({ generation: '2', code: 'provider_unavailable' }),
    )
    vi.mocked(transport.mutations.request).mockImplementationOnce(
      async (_path, _body, sent) =>
        new Promise<Response>((_resolve, reject) =>
          sent?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
        ),
    )
    void controller.refresh()
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledTimes(3))

    controller.cancel()
    await vi.waitFor(() => expect(controller.state.dialog).toBeUndefined())

    expect(controller.state.announcement).toBe(REFRESH_STOPPED)
    expect(notices()).toEqual({ banners: [], status: '' })
  })

  it('keeps a canceled attempt unclaimed when status polling failed', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const host = hostStub(current)
    const transport = transportStub()
    vi.mocked(transport.mutations.request).mockImplementation(
      async (_path, _body, sent) =>
        new Promise<Response>((_resolve, reject) =>
          sent?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
        ),
    )
    const controller = createProviderController({ transport, host })
    controller.sync(current)
    await controller.poll()
    vi.mocked(transport.status).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    await controller.poll()

    void controller.refresh()
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledOnce())
    vi.mocked(transport.status).mockResolvedValue(providerStatus({ generation: '2' }))
    controller.cancel()
    await vi.waitFor(() => expect(controller.state.dialog).toBeUndefined())

    expect(notices()).toEqual({ banners: [], status: '' })
    expect(controller.state.staleReason).toBe(REFRESH_STOPPED)
    expect(controller.state.announcement).not.toContain('no provider data changed')
    expect(host.reload).toHaveBeenCalledOnce()
  })

  it('opens the dialog for a due refresh before the first view arrives', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const host = hostStub(current)
    host.current.mockReturnValue(undefined as unknown as ViewProjection)
    const transport = transportStub({
      status: providerStatus({ provider_kind: 'monarch', refresh_due: 'recent' }),
      mutationResponse: Response.json(refreshResponse(current, 'preserved')),
    })
    const controller = createProviderController({ transport, host })

    await controller.poll()
    expect(controller.state.dialog?.view).toBe('progress')
    expect(controller.state.kind).toBe('recent')
    expect(transport.mutations.request).not.toHaveBeenCalled()

    host.current.mockReturnValue(current)
    controller.sync(current)
    await vi.waitFor(() => expect(controller.state.dialog).toBeUndefined())
    expect(JSON.parse(vi.mocked(transport.mutations.request).mock.calls[0]![1]).manual).toBe(false)
  })

  it('reports a status request that failed', async () => {
    const transport = transportStub()
    vi.mocked(transport.status).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    const controller = createProviderController({ transport, host: hostStub() })

    await controller.poll()

    expect(controller.state.statusError).toBe(
      'Moneyflow could not reach the server. Check the connection and retry.',
    )
    await controller.poll()
    expect(controller.state.statusError).toBeUndefined()
  })

  it('asks for confirmation without an error banner', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const pending = providerStatus({
      code: 'provider_deletion_confirmation_required',
      confirmation_token: 'opaque-confirmation',
    })
    const transport = transportStub({
      mutationResponse: problemResponse(
        'provider_deletion_confirmation_required',
        'Confirm provider removals.',
        pending,
      ),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)

    await controller.refresh()

    expect(controller.state.phase).toBe('confirmation')
    expect(controller.state.dialog?.view).toBe('confirmation')
    expect(notices()).toEqual({ banners: [], status: '' })

    controller.dismissConfirmation()
    expect(controller.state.dialog).toBeUndefined()
    expect(controller.state.staleReason).toBe(REFRESH_NOT_CONFIRMED)
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
    expect(JSON.parse(vi.mocked(transport.mutations.request).mock.calls[0]![1]).manual).toBe(false)
    expect(controller.state.phase).toBe('idle')
  })

  it('leaves a due refresh to the server when autoRefresh is off', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      status: providerStatus({ provider_kind: 'monarch', refresh_due: 'recent' }),
    })
    const controller = createProviderController({
      transport,
      host: hostStub(current),
      autoRefresh: false,
    })
    controller.sync(current)

    await controller.poll()

    expect(transport.mutations.request).not.toHaveBeenCalled()
    expect(controller.state.phase).toBe('idle')
  })

  it('follows a refresh it did not start without a dialog in a host', async () => {
    vi.useFakeTimers()
    try {
      const current = testProjection({ capabilities: [capability(true)] })
      const host = hostStub(current)
      const transport = transportStub({ status: providerStatus({ refreshing: true }) })
      const controller = createProviderController({ transport, host, autoRefresh: false })
      controller.sync(current)

      await controller.poll()

      expect(controller.state.dialog).toBeUndefined()
      expect(controller.state.phase).toBe('refreshing')
      expect(controller.state.announcement).toBe('Moneyflow is refreshing this profile.')
      transport.status.mockResolvedValue(
        providerStatus({
          generation: '2',
          summary: { ...emptySummary(), imported_transactions: 7 },
        }),
      )
      await vi.advanceTimersByTimeAsync(1_000)
      expect(host.reload).toHaveBeenCalledOnce()
      expect(controller.state.phase).toBe('idle')
      expect(notices()).toEqual({ banners: [], status: 'Refresh complete: 7 imported, 0 removed.' })
    } finally {
      vi.useRealTimers()
    }
  })

  it('opens no dialog for a refresh starting as its own ends, in a host', async () => {
    vi.useFakeTimers()
    try {
      const current = testProjection({ capabilities: [capability(true)] })
      const response = refreshResponse(current, 'preserved')
      response.status = providerStatus({ revision: '0', generation: '2', refreshing: true })
      const transport = transportStub({ mutationResponse: Response.json(response) })
      const controller = createProviderController({
        transport,
        host: hostStub(current),
        autoRefresh: false,
      })
      controller.sync(current)

      await controller.refresh()

      expect(controller.state.dialog).toBeUndefined()
      expect(controller.state.phase).toBe('refreshing')
    } finally {
      vi.useRealTimers()
    }
  })

  it('follows its own refresh past a 503 without status, in a host', async () => {
    vi.useFakeTimers()
    try {
      const current = testProjection({ capabilities: [capability(true)] })
      const host = hostStub(current)
      const answer = Promise.withResolvers<Response>()
      const transport = transportStub()
      transport.mutations.request.mockReturnValue(answer.promise)
      const controller = createProviderController({ transport, host, autoRefresh: false })
      controller.sync(current)

      const refreshing = controller.refresh()
      await vi.advanceTimersByTimeAsync(1_000)
      expect(controller.state.dialog?.view).toBe('progress')
      answer.resolve(
        problemResponse(
          'provider_refresh_in_progress',
          'The refresh is still running.',
          undefined,
          503,
        ),
      )
      await refreshing
      transport.status.mockResolvedValue(providerStatus({ refreshing: true }))
      await vi.advanceTimersByTimeAsync(1_000)

      expect(controller.state.dialog?.view).toBe('progress')
      expect(controller.state.phase).toBe('refreshing')
      transport.status.mockResolvedValue(
        providerStatus({
          generation: '2',
          summary: { ...emptySummary(), imported_transactions: 7 },
        }),
      )
      await vi.advanceTimersByTimeAsync(1_000)
      expect(controller.state.dialog).toBeUndefined()
      expect(host.reload).toHaveBeenCalledOnce()
      expect(notices()).toEqual({ banners: [], status: 'Refresh complete: 7 imported, 0 removed.' })
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps a refresh asked for before the first view in its dialog, in a host', async () => {
    vi.useFakeTimers()
    try {
      const shown = vi.fn((): ViewProjection | undefined => undefined)
      const host = { ...hostStub(), current: shown }
      const transport = transportStub()
      transport.mutations.request.mockReturnValue(new Promise<Response>(() => undefined))
      const controller = createProviderController({ transport, host, autoRefresh: false })
      await controller.poll()
      await controller.refresh()

      await controller.poll()

      expect(controller.state.dialog?.view).toBe('progress')
      expect(controller.state.phase).toBe('refreshing')
      const current = testProjection({ capabilities: [capability(true)] })
      shown.mockReturnValue(current)
      controller.sync(current)
      expect(transport.mutations.request).toHaveBeenCalledOnce()
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps a failed refresh open when asked to hide its dialog', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub({
      mutationResponse: problemResponse('provider_rate_limited', 'Rate limited.'),
    })
    const controller = createProviderController({
      transport,
      host: hostStub(current),
      autoRefresh: false,
    })
    controller.sync(current)
    await controller.refresh()

    controller.hide()

    expect(controller.state.dialog?.view).toBe('failed')
  })

  it('retries complete history when a refresh fails after its dialog closed', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const answer = Promise.withResolvers<Response>()
    const transport = transportStub()
    transport.mutations.request.mockReturnValue(answer.promise)
    const controller = createProviderController({
      transport,
      host: hostStub(current),
      autoRefresh: false,
    })
    controller.sync(current)
    const refreshing = controller.refresh(true)
    controller.hide()

    answer.resolve(problemResponse('provider_rate_limited', 'Rate limited.'))
    await refreshing

    expect(controller.state.dialog?.view).toBe('failed')
    expect(controller.state.dialog?.completeHistory).toBe(true)
  })

  it('follows the refresh after a 503 without status, as another one, in standalone', async () => {
    vi.useFakeTimers()
    try {
      const current = testProjection({ capabilities: [capability(true)] })
      const host = hostStub(current)
      const transport = transportStub({
        status: providerStatus({ refreshing: true }),
        mutationResponse: problemResponse(
          'provider_refresh_in_progress',
          'A refresh is already running.',
          undefined,
          503,
        ),
      })
      const controller = createProviderController({ transport, host, standalone: true })
      controller.sync(current)

      await controller.refresh()
      await vi.advanceTimersByTimeAsync(1_000)

      expect(controller.state.dialog?.view).toBe('other')
      transport.status.mockResolvedValue(
        providerStatus({
          generation: '2',
          summary: { ...emptySummary(), imported_transactions: 7 },
        }),
      )
      await vi.advanceTimersByTimeAsync(1_000)
      expect(controller.state.dialog).toBeUndefined()
      expect(host.reload).toHaveBeenCalledOnce()
      expect(notices()).toEqual({ banners: [], status: 'Refresh complete: 7 imported, 0 removed.' })
    } finally {
      vi.useRealTimers()
    }
  })

  it('confirms with the token of a parked refresh it did not start', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const parked = providerStatus({
      code: 'provider_deletion_confirmation_required',
      confirmation_token: 'parked-attempt-token',
      summary: { ...emptySummary(), removed_transactions: 3 },
    })
    const transport = transportStub({
      mutationResponse: problemResponse(
        'provider_deletion_confirmation_required',
        'This refresh would remove 3 posted transactions.',
        parked,
      ),
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)
    await controller.poll()

    await controller.refresh()

    expect(controller.state.dialog?.view).toBe('confirmation')
    expect(controller.state.status).toEqual(parked)
    vi.mocked(transport.mutations.request).mockResolvedValueOnce(
      Response.json(refreshResponse(current, 'preserved')),
    )
    await controller.confirm()
    const [path, body] = vi.mocked(transport.mutations.request).mock.calls[1]!
    expect(path).toBe('api/v1/provider/refresh/confirm')
    expect(JSON.parse(body).confirmation_token).toBe('parked-attempt-token')
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
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledOnce())
    expect(controller.state.kind).toBe('recent')
    finish(
      Response.json({
        ...refreshResponse(current, 'preserved'),
        status: providerStatus({ provider_kind: 'monarch', manual_refresh: 'recent' }),
      }),
    )
    await scheduled
    expect(controller.state.kind).toBeUndefined()

    const manual = controller.refresh()
    expect(controller.state.kind).toBe('recent')
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledTimes(2))
    expect(JSON.parse(vi.mocked(transport.mutations.request).mock.calls[1]![1])).not.toHaveProperty(
      'complete_history',
    )
    finish(
      Response.json({
        ...refreshResponse(current, 'preserved'),
        status: providerStatus({ provider_kind: 'monarch', manual_refresh: 'recent' }),
      }),
    )
    await manual

    const complete = controller.refresh(true)
    expect(controller.state.kind).toBe('full')
    expect(controller.state.dialog?.completeHistory).toBe(true)
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledTimes(3))
    expect(JSON.parse(vi.mocked(transport.mutations.request).mock.calls[2]![1])).toMatchObject({
      manual: true,
      complete_history: true,
    })
    finish(Response.json(refreshResponse(current, 'preserved')))
    await complete
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
      await vi.advanceTimersByTimeAsync(0)
      const started = transport.status.mock.calls.length
      await vi.advanceTimersByTimeAsync(2_000)
      expect(transport.status).toHaveBeenCalledTimes(started + 2)

      finish(Response.json(refreshResponse(current, 'preserved')))
      await refreshing
      await vi.advanceTimersByTimeAsync(5_000)
      expect(transport.status).toHaveBeenCalledTimes(started + 2)
      expect(controller.state.phase).toBe('idle')
    } finally {
      vi.useRealTimers()
    }
  })

  it.each([undefined, 'provider_unavailable', 'provider_reconnect_required'])(
    'follows another interface refresh after %s without starting a competing one',
    async (code) => {
      const current = testProjection({ capabilities: [capability(true)] })
      const host = hostStub(current)
      const transport = transportStub({
        status: providerStatus({
          refreshing: true,
          owner_renderer: 'tui',
          refresh_due: 'full',
          progress: { fetched: 20, total: 40, verifying: false, percent: 25 },
        }),
      })
      let now = 1_000
      const controller = createProviderController({
        transport,
        host,
        now: () => now,
        standalone: true,
      })
      controller.sync(current)
      if (code) {
        transport.mutations.request.mockResolvedValueOnce(problemResponse(code, 'Unavailable.'))
        await controller.refresh()
      }
      const requests = transport.mutations.request.mock.calls.length
      now = 2_000

      await controller.poll()

      expect(controller.state.phase).toBe('refreshing')
      expect(controller.state.dialog?.view).toBe('other')
      expect(controller.state.dialog?.startedAt).toBe(2_000)
      expect(controller.state.status?.progress).toEqual({
        fetched: 20,
        total: 40,
        verifying: false,
        percent: 25,
      })
      expect(controller.state.announcement).toBe(
        'The terminal app (moneyflow tui) is refreshing this profile.',
      )
      expect(transport.mutations.request).toHaveBeenCalledTimes(requests)

      vi.mocked(transport.status).mockResolvedValueOnce(
        providerStatus({
          generation: '2',
          summary: { ...emptySummary(), imported_transactions: 7 },
        }),
      )
      await controller.poll()
      await vi.waitFor(() => expect(controller.state.phase).toBe('idle'))
      expect(controller.state.dialog).toBeUndefined()
      expect(host.reload).toHaveBeenCalledOnce()
      expect(controller.state.announcement).toBe('Refresh complete: 7 imported, 0 removed.')
    },
  )

  it.each(['open', 'continued'] as const)(
    'clears a %s failure once another interface refreshes successfully',
    async (failure) => {
      const current = testProjection({ capabilities: [capability(true)] })
      const host = hostStub(current)
      const failedStatus = providerStatus({
        code: 'provider_unavailable',
        last_success: '2026-10-03T12:00:00Z',
      })
      const transport = transportStub({
        mutationResponse: problemResponse('provider_unavailable', 'Unavailable.', failedStatus),
      })
      const controller = createProviderController({ transport, host })
      controller.sync(current)
      await controller.refresh()
      if (failure === 'continued') controller.continueStale()

      vi.mocked(transport.status).mockResolvedValue(
        providerStatus({ generation: '2', last_success: '2026-10-13T12:00:00Z' }),
      )
      await controller.poll()

      expect(controller.state.dialog).toBeUndefined()
      expect(controller.state.staleReason).toBeUndefined()
      expect(controller.state.phase).toBe('idle')
      expect(host.reload).toHaveBeenCalledOnce()
      expect(transport.mutations.request).toHaveBeenCalledOnce()
    },
  )

  it('keeps loading newer data until a reread installs it', async () => {
    const shown = testProjection({ revision: '1', capabilities: [capability(true)] })
    const host = hostStub(shown)
    const transport = transportStub({ status: providerStatus({ revision: '1' }) })
    const controller = createProviderController({ transport, host })
    controller.sync(shown)
    await controller.poll()
    vi.mocked(transport.status).mockResolvedValue(
      providerStatus({ generation: '2', revision: '2' }),
    )

    // The reread answers, but a view change that started meanwhile owns the view and fails, so
    // the view stays at revision 1.
    await controller.poll()
    await vi.waitFor(() => expect(host.reload).toHaveBeenCalledOnce())
    await settle()
    expect(staleNotice(controller.state, Date.now())?.text).toBe('Newer provider data is loading.')

    host.reload.mockImplementation(async () => {
      host.current.mockReturnValue(testProjection({ revision: '2' }))
      return undefined
    })
    await controller.poll()
    await vi.waitFor(() => expect(host.reload).toHaveBeenCalledTimes(2))
    await settle()
    expect(staleNotice(controller.state, Date.now())).toBeUndefined()
  })

  it('rereads when the first status is newer than the initial view', async () => {
    const host = hostStub(testProjection({ revision: '1' }))
    const transport = transportStub({ status: providerStatus({ generation: '2', revision: '2' }) })
    host.reload.mockRejectedValue(new Error('offline'))
    const controller = createProviderController({ transport, host })
    controller.sync(host.current())

    await controller.poll()
    await settle()

    expect(host.reload).toHaveBeenCalledOnce()
    expect(staleNotice(controller.state, Date.now())?.rereadFailed).toBe(true)
  })

  it('rereads a restored search projection after the newest data was already shown', async () => {
    const host = hostStub(testProjection({ revision: '2' }))
    const transport = transportStub({ status: providerStatus({ generation: '2', revision: '2' }) })
    const controller = createProviderController({ transport, host })
    controller.sync(host.current())
    await controller.poll()
    host.current.mockReturnValue(testProjection({ revision: '1' }))

    controller.sync(host.current())
    await settle()

    expect(host.reload).toHaveBeenCalledOnce()
    expect(staleNotice(controller.state, Date.now())?.text).toBe('Newer provider data is loading.')
  })

  it.each([false, true])(
    'ignores a delayed poll after a newer poll (reread fails: %s)',
    async (fails) => {
      const host = hostStub(testProjection({ revision: '1' }))
      if (fails) host.reload.mockRejectedValue(new Error('offline'))
      const transport = transportStub({ status: providerStatus({ revision: '1' }) })
      const controller = createProviderController({ transport, host })
      await controller.poll()
      const older = Promise.withResolvers<ProviderStatus>()
      transport.status.mockReturnValueOnce(older.promise)
      const delayed = controller.poll()
      transport.status.mockResolvedValue(providerStatus({ generation: '3', revision: '3' }))
      await controller.poll()
      await settle()
      const notice = staleNotice(controller.state, Date.now())

      older.resolve(providerStatus({ generation: '2', revision: '1' }))
      await delayed

      expect(controller.state.status?.generation).toBe('3')
      expect(staleNotice(controller.state, Date.now())).toEqual(notice)
      expect(notice).toBeDefined()
    },
  )

  it.each(['success', 'later failure'] as const)(
    'clears a continued failure when a watched refresh saves data, then polls %s during reread',
    async (outcome) => {
      const current = testProjection({ revision: '1', capabilities: [capability(true)] })
      const host = hostStub(current)
      const reread = Promise.withResolvers<undefined>()
      host.reload.mockReturnValueOnce(reread.promise)
      const transport = transportStub({
        status: providerStatus({ revision: '1' }),
        mutationResponse: problemResponse('provider_rate_limited', 'Rate limited.'),
      })
      const controller = createProviderController({ transport, host, standalone: true })
      controller.sync(current)
      await controller.poll()
      await controller.refresh()
      controller.continueStale()
      expect(staleNotice(controller.state, Date.now())?.text).toContain('Rate limited.')

      transport.status.mockResolvedValue(providerStatus({ refreshing: true, revision: '1' }))
      await controller.poll()
      expect(controller.state.dialog?.view).toBe('other')
      transport.status.mockResolvedValue(providerStatus({ generation: '2', revision: '2' }))
      await controller.poll()
      transport.status.mockResolvedValue(
        providerStatus({
          generation: '2',
          revision: '2',
          ...(outcome === 'later failure' ? { code: 'provider_unavailable' } : {}),
        }),
      )
      await controller.poll()

      host.current.mockReturnValue(testProjection({ revision: '2' }))
      reread.resolve(undefined)
      await settle()

      const notice = staleNotice(controller.state, Date.now())
      if (outcome === 'later failure') {
        expect(controller.state.status?.code).toBe('provider_unavailable')
        expect(notice?.text).toContain('Last refresh failed.')
        expect(notice?.text).not.toContain('Rate limited.')
      } else {
        expect(notice).toBeUndefined()
      }
    },
  )

  it('keeps newer status when a watched refresh finishes rereading', async () => {
    const host = hostStub(testProjection({ revision: '1' }))
    const reread = Promise.withResolvers<undefined>()
    host.reload.mockReturnValueOnce(reread.promise)
    const transport = transportStub({ status: providerStatus({ refreshing: true, revision: '1' }) })
    const controller = createProviderController({ transport, host, standalone: true })
    await controller.poll()
    transport.status.mockResolvedValue(providerStatus({ generation: '2', revision: '2' }))
    await controller.poll()
    transport.status.mockResolvedValue(providerStatus({ generation: '3', revision: '3' }))
    await controller.poll()

    host.current.mockReturnValue(testProjection({ revision: '2' }))
    reread.resolve(undefined)
    await settle()

    expect(controller.state.status?.generation).toBe('3')
    expect(staleNotice(controller.state, Date.now())?.text).toBe('Newer provider data is loading.')
    expect(notices()).toEqual({ banners: [], status: 'Refresh complete: 0 imported, 0 removed.' })
  })

  it('keeps a newer failure when a successful response finishes late', async () => {
    const current = testProjection({ revision: '1', capabilities: [capability(true)] })
    const host = hostStub(current)
    host.reload.mockRejectedValue(new Error('offline'))
    const response = Promise.withResolvers<Response>()
    const transport = transportStub()
    transport.mutations.request.mockReturnValueOnce(response.promise)
    const controller = createProviderController({ transport, host })
    controller.sync(current)
    const refresh = controller.refresh()
    transport.status.mockResolvedValue(
      providerStatus({
        generation: '3',
        revision: '3',
        code: 'provider_unavailable',
      }),
    )
    await controller.poll()

    response.resolve(Response.json(refreshResponse(testProjection({ revision: '2' }), 'preserved')))
    await refresh
    await settle()

    expect(controller.state.status).toMatchObject({ generation: '3', code: 'provider_unavailable' })
    expect(host.accept).not.toHaveBeenCalled()
    expect(staleNotice(controller.state, Date.now())?.rereadFailed).toBe(true)
    expect(notices()).toEqual({
      banners: [],
      status: 'Refresh complete: 0 imported, 0 removed. The current view could not be reread.',
    })
  })

  it('ignores a delayed poll even when the generation and revision have not changed', async () => {
    const transport = transportStub()
    const controller = createProviderController({ transport, host: hostStub() })
    const delayed = Promise.withResolvers<ProviderStatus>()
    transport.status.mockReturnValueOnce(delayed.promise)
    const poll = controller.poll()
    transport.status.mockResolvedValue(providerStatus({ code: 'provider_unavailable' }))
    await controller.poll()
    delayed.resolve(providerStatus())
    await poll

    expect(controller.state.status?.code).toBe('provider_unavailable')
    expect(staleNotice(controller.state, Date.now())?.text).toContain('Last refresh failed.')
  })

  it('accepts a slow poll while the following poll is still pending', async () => {
    const transport = transportStub({ status: providerStatus({ refreshing: true }) })
    const controller = createProviderController({ transport, host: hostStub(), standalone: true })
    await controller.poll()
    const first = Promise.withResolvers<ProviderStatus>()
    const second = Promise.withResolvers<ProviderStatus>()
    transport.status.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const firstPoll = controller.poll()
    const secondPoll = controller.poll()

    first.resolve(providerStatus({ code: 'provider_unavailable' }))
    await firstPoll
    expect(controller.state.dialog).toBeUndefined()

    second.resolve(providerStatus({ code: 'provider_unavailable' }))
    await secondPoll
  })

  it('keeps a newer status-check failure when a completed refresh finishes rereading', async () => {
    const host = hostStub(testProjection({ revision: '1' }))
    const reread = Promise.withResolvers<undefined>()
    host.reload.mockReturnValueOnce(reread.promise)
    const transport = transportStub({ status: providerStatus({ refreshing: true, revision: '1' }) })
    const controller = createProviderController({ transport, host, standalone: true })
    await controller.poll()
    transport.status.mockResolvedValue(providerStatus({ generation: '2', revision: '2' }))
    await controller.poll()
    transport.status.mockRejectedValueOnce(new Error('offline'))
    await controller.poll()

    host.current.mockReturnValue(testProjection({ revision: '2' }))
    reread.resolve(undefined)
    await settle()

    expect(controller.state.statusError).toBeDefined()
    expect(staleNotice(controller.state, Date.now())?.checkFailed).toBe(true)
  })

  it('keeps a newly observed lease when its own completed response finishes displaying', async () => {
    const current = testProjection({ revision: '1', capabilities: [capability(true)] })
    const host = hostStub(current)
    const viewChange = Promise.withResolvers<void>()
    host.serially = (work) => viewChange.promise.then(work)
    const transport = transportStub({
      mutationResponse: Response.json(
        refreshResponse(testProjection({ revision: '2' }), 'preserved'),
      ),
    })
    const controller = createProviderController({ transport, host, standalone: true })
    controller.sync(current)
    const refresh = controller.refresh()
    await settle()
    transport.status.mockResolvedValue(
      providerStatus({ generation: '2', revision: '2', refreshing: true }),
    )
    await controller.poll()

    viewChange.resolve()
    await refresh

    expect(controller.state.status?.refreshing).toBe(true)
    expect(controller.state.dialog?.view).toBe('other')
    transport.status.mockResolvedValue(providerStatus({ generation: '2', revision: '2' }))
    await controller.poll()
  })

  it('preserves a newer reconnect status when an older failed response arrives', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const transport = transportStub()
    const response = Promise.withResolvers<Response>()
    transport.mutations.request.mockReturnValueOnce(response.promise)
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)
    const refresh = controller.refresh()
    transport.status.mockResolvedValue(
      providerStatus({
        code: 'provider_reconnect_required',
        last_attempt: '2026-10-13T12:01:00Z',
      }),
    )
    await controller.poll()

    response.resolve(
      problemResponse(
        'provider_unavailable',
        'Unavailable.',
        providerStatus({
          code: 'provider_unavailable',
          last_attempt: '2026-10-13T12:00:00Z',
        }),
      ),
    )
    await refresh
    controller.continueStale()

    expect(controller.state.status?.code).toBe('provider_reconnect_required')
    expect(staleNotice(controller.state, Date.now())?.reconnect).toBe(true)
  })

  it.each([
    { provider_kind: 'simplefin', polledAttempt: '12:00', refreshing: true, dialog: 'failed' },
    { provider_kind: 'simplefin', polledAttempt: '13:00', refreshing: true, dialog: 'other' },
    { provider_kind: 'simplefin', polledAttempt: '13:00', refreshing: false, dialog: undefined },
    { provider_kind: 'monarch', polledAttempt: '12:00', refreshing: true, dialog: 'other' },
  ] as const)(
    'handles a $provider_kind failure from 12:00 after polling $polledAttempt (running: $refreshing)',
    async ({ provider_kind, polledAttempt, refreshing, dialog }) => {
      const current = testProjection({ revision: '1', capabilities: [capability(true)] })
      const transport = transportStub({
        status: providerStatus({
          provider_kind,
          revision: '1',
          last_attempt: '2026-10-13T10:00:00Z',
        }),
      })
      const response = Promise.withResolvers<Response>()
      transport.mutations.request.mockReturnValueOnce(response.promise)
      const controller = createProviderController({
        transport,
        host: hostStub(current),
        standalone: true,
      })
      controller.sync(current)
      await controller.poll()
      const refresh = controller.refresh()
      const polled = providerStatus({
        provider_kind,
        revision: '1',
        last_attempt: `2026-10-13T${polledAttempt}:00Z`,
        refreshing,
        ...(refreshing ? {} : { code: 'provider_reconnect_required' }),
      })
      transport.status.mockResolvedValue(polled)
      await controller.poll()

      const failed = providerStatus({
        provider_kind,
        revision: '1',
        last_attempt: '2026-10-13T12:00:00Z',
        code: 'provider_unavailable',
      })
      response.resolve(problemResponse('provider_unavailable', 'Unavailable.', failed))
      await refresh

      expect(controller.state.dialog?.view).toBe(dialog)
      if (dialog === 'failed') {
        expect(controller.state.dialog?.reason).toBe('Unavailable.')
        transport.status.mockResolvedValue(failed)
        await controller.poll()
        expect(controller.state.dialog?.view).toBe('failed')
      } else {
        expect(controller.state.status).toEqual(polled)
        // Finish the external lease so its progress polling ends.
        transport.status.mockResolvedValue({ ...polled, refreshing: false })
        await controller.poll()
      }
    },
  )

  it.each<{
    phase: 'running' | 'finished'
    revision: string
    newer?: 'generation' | 'attempt'
  }>([
    { phase: 'running', revision: '2' },
    { phase: 'finished', revision: '2' },
    { phase: 'running', revision: '3' },
    { phase: 'finished', revision: '3' },
    { phase: 'finished', revision: '3', newer: 'generation' },
    { phase: 'finished', revision: '3', newer: 'attempt' },
  ])(
    'handles deletion confirmation after polling $phase at revision $revision (newer: $newer)',
    async ({ phase, revision, newer }) => {
      const current = testProjection({ revision: '1', capabilities: [capability(true)] })
      const transport = transportStub({
        status: providerStatus({ revision: '1', last_attempt: '2026-10-13T12:00:00Z' }),
      })
      const response = Promise.withResolvers<Response>()
      transport.mutations.request.mockReturnValueOnce(response.promise)
      const controller = createProviderController({ transport, host: hostStub(current) })
      controller.sync(current)
      await controller.poll()
      const refresh = controller.refresh()
      transport.status.mockResolvedValue(
        providerStatus({ revision, refreshing: true, last_attempt: '2026-10-13T12:00:00Z' }),
      )
      await controller.poll()
      if (phase === 'finished') {
        transport.status.mockResolvedValue(
          providerStatus({
            revision,
            generation: newer === 'generation' ? '2' : '1',
            code: 'provider_deletion_confirmation_required',
            last_attempt: newer === 'attempt' ? '2026-10-13T12:02:00Z' : '2026-10-13T12:01:00Z',
          }),
        )
        await controller.poll()
      }
      response.resolve(
        problemResponse(
          'provider_deletion_confirmation_required',
          'Confirm removals.',
          providerStatus({
            revision: '2',
            code: 'provider_deletion_confirmation_required',
            last_attempt: '2026-10-13T12:01:00Z',
            confirmation_token: 'candidate-after-progress',
            summary: { ...emptySummary(), removed_transactions: 5 },
          }),
        ),
      )
      await refresh

      expect(controller.state.status?.revision).toBe(revision)
      if (newer) {
        expect(controller.state.dialog).toBeUndefined()
        expect(controller.state.status?.confirmation_token).toBeUndefined()
      } else {
        expect(controller.state.dialog?.view).toBe('confirmation')
        expect(controller.state.status?.confirmation_token).toBe('candidate-after-progress')
        expect(controller.state.status?.summary.removed_transactions).toBe(5)
      }
    },
  )

  it('does not credit an intervening refresh to a canceled request', async () => {
    const current = testProjection({ revision: '1', capabilities: [capability(true)] })
    const transport = transportStub({ status: providerStatus({ revision: '1' }) })
    transport.mutations.request.mockImplementation(async (_path, _body, signal) => {
      // Another process commits after the preceding status read, before this request is admitted.
      transport.status.mockResolvedValue(
        providerStatus({
          generation: '2',
          revision: '2',
          code: 'provider_unavailable',
          summary: { ...emptySummary(), imported_transactions: 40 },
        }),
      )
      return new Promise<Response>((_resolve, reject) =>
        signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
      )
    })
    const controller = createProviderController({ transport, host: hostStub(current) })
    controller.sync(current)
    await controller.poll()
    const refresh = controller.refresh()
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledOnce())

    controller.cancel()
    await refresh
    await settle()

    expect(notices()).toEqual({ banners: [], status: '' })
    expect(controller.state.announcement).not.toContain('no provider data changed')
    expect(controller.state.dialog).toBeUndefined()
  })

  it('rereads the view when a canceled refresh finds newer saved data', async () => {
    const shown = testProjection({ revision: '1', capabilities: [capability(true)] })
    const host = hostStub(shown)
    const transport = transportStub({ status: providerStatus({ revision: '1' }) })
    vi.mocked(transport.mutations.request).mockImplementation(
      async (_path, _body, sent) =>
        new Promise<Response>((_resolve, reject) =>
          sent?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
        ),
    )
    const controller = createProviderController({ transport, host })
    controller.sync(shown)
    await controller.poll()
    vi.mocked(transport.status).mockResolvedValue(
      providerStatus({
        generation: '2',
        revision: '2',
        code: 'provider_unavailable',
        summary: { ...emptySummary(), imported_transactions: 40 },
      }),
    )

    void controller.refresh()
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledOnce())
    controller.cancel()
    await vi.waitFor(() => expect(controller.state.dialog).toBeUndefined())

    await vi.waitFor(() => expect(host.reload).toHaveBeenCalledOnce())
    expect(controller.state.announcement).toBe(REFRESH_STOPPED)
    expect(notices()).toEqual({ banners: [], status: '' })
  })

  it('keeps the failure when an older reread succeeds after a newer one failed', async () => {
    const shown = testProjection({ revision: '1', capabilities: [capability(true)] })
    const host = hostStub(shown)
    let finishOlder: () => void = () => undefined
    host.reload
      .mockImplementationOnce(
        () =>
          new Promise<undefined>((resolve) => {
            finishOlder = () => {
              host.current.mockReturnValue(testProjection({ revision: '2' }))
              resolve(undefined)
            }
          }),
      )
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
    const transport = transportStub({ status: providerStatus({ revision: '1' }) })
    const controller = createProviderController({ transport, host })
    controller.sync(shown)
    await controller.poll()
    vi.mocked(transport.status).mockResolvedValue(
      providerStatus({ generation: '2', revision: '3' }),
    )

    await controller.poll()
    await vi.waitFor(() => expect(host.reload).toHaveBeenCalledOnce())
    await controller.reread()
    expect(staleNotice(controller.state, Date.now())?.rereadFailed).toBe(true)

    finishOlder()
    await settle()
    expect(staleNotice(controller.state, Date.now())?.rereadFailed).toBe(true)
  })

  it('keeps a visible notice until data refreshed elsewhere loads', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const host = hostStub(current)
    host.reload.mockRejectedValue(new TypeError('Failed to fetch'))
    const transport = transportStub()
    const controller = createProviderController({ transport, host })
    controller.sync(current)
    await controller.poll()
    vi.mocked(transport.status).mockResolvedValue(providerStatus({ generation: '2' }))

    await controller.poll()
    await vi.waitFor(() => expect(host.reload).toHaveBeenCalledOnce())
    await vi.waitFor(() =>
      expect(staleNotice(controller.state, Date.now())).toMatchObject({
        text:
          'Refreshed provider data could not be loaded. ' +
          'Moneyflow could not reach the server. Check the connection and retry.',
        rereadFailed: true,
      }),
    )
    await controller.poll()
    await vi.waitFor(() => expect(host.reload).toHaveBeenCalledTimes(2))
    expect(staleNotice(controller.state, Date.now())?.rereadFailed).toBe(true)

    await controller.reread()
    expect(staleNotice(controller.state, Date.now())?.rereadFailed).toBe(true)
    host.reload.mockImplementation(async () => {
      host.current.mockReturnValue(testProjection({ revision: '1' }))
      return undefined
    })
    await controller.reread()
    expect(staleNotice(controller.state, Date.now())).toBeUndefined()
    expect(host.reload).toHaveBeenCalledTimes(4)
  })

  it('keeps a failure while the status shows no newer data', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const host = hostStub(current)
    const failedStatus = providerStatus({
      code: 'provider_unavailable',
      last_success: '2026-10-03T12:00:00Z',
    })
    const transport = transportStub({
      status: failedStatus,
      mutationResponse: problemResponse('provider_unavailable', 'Unavailable.', failedStatus),
    })
    const controller = createProviderController({ transport, host })
    controller.sync(current)
    await controller.refresh()
    controller.continueStale()

    await controller.poll()

    expect(controller.state.staleReason).toBe('Unavailable.')
    expect(host.reload).not.toHaveBeenCalled()
  })

  it('keeps a new refresh running when the previous refresh finishes its reread', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const host = hostStub(current)
    let reread: () => void = () => undefined
    host.reload.mockImplementationOnce(
      () => new Promise<undefined>((resolve) => (reread = () => resolve(undefined))),
    )
    const transport = transportStub({ status: providerStatus({ refreshing: true }) })
    vi.mocked(transport.mutations.request).mockImplementation(
      async (_path, _body, sent) =>
        new Promise<Response>((_resolve, reject) =>
          sent?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
        ),
    )
    const controller = createProviderController({ transport, host, standalone: true })
    controller.sync(current)
    await controller.poll()
    vi.mocked(transport.status).mockResolvedValue(
      providerStatus({ generation: '2', summary: { ...emptySummary(), imported_transactions: 7 } }),
    )
    await controller.poll()
    expect(controller.state.dialog).toBeUndefined()

    void controller.refresh()
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledOnce())
    reread()
    await vi.waitFor(() => expect(notices().status).not.toBe(''))

    expect(controller.state.dialog?.view).toBe('progress')
    expect(controller.state.phase).toBe('refreshing')
    expect(notices()).toEqual({ banners: [], status: 'Refresh complete: 7 imported, 0 removed.' })
    controller.cancel()
  })

  it('closes the dialog when another interface refresh stops without new data', async () => {
    const transport = transportStub({ status: providerStatus({ refreshing: true }) })
    const controller = createProviderController({ transport, host: hostStub(), standalone: true })
    await controller.poll()
    vi.mocked(transport.status).mockResolvedValueOnce(
      providerStatus({ code: 'provider_unavailable', last_success: '2026-10-03T12:00:00Z' }),
    )

    await controller.poll()

    expect(controller.state.dialog).toBeUndefined()
    expect(controller.state.phase).toBe('failed')
  })

  describe('once stopped', () => {
    it('shows nothing from a refresh that answers after it stopped', async () => {
      const current = testProjection({ capabilities: [capability(true)] })
      const host = hostStub(current)
      const transport = transportStub()
      const answer = Promise.withResolvers<Response>()
      transport.mutations.request.mockReturnValueOnce(answer.promise)
      const controller = createProviderController({ transport, host })
      controller.sync(current)

      const refreshing = controller.refresh()
      controller.stop()
      answer.resolve(Response.json(refreshResponse(current, 'preserved')))
      await refreshing
      await settle()

      expect({
        installed: host.accept.mock.calls.length,
        reread: host.reload.mock.calls.length,
        notices: notices(),
      }).toEqual({ installed: 0, reread: 0, notices: { banners: [], status: '' } })
    })

    it('shows nothing from a refresh that fails after it stopped', async () => {
      const current = testProjection({ capabilities: [capability(true)] })
      const transport = transportStub()
      const answer = Promise.withResolvers<Response>()
      transport.mutations.request.mockReturnValueOnce(answer.promise)
      const controller = createProviderController({ transport, host: hostStub(current) })
      controller.sync(current)
      const refreshing = controller.refresh()
      // A status check while the refresh runs is newer than the failure that answers it.
      transport.status.mockResolvedValue(providerStatus({ last_attempt: '2026-10-13T12:01:00Z' }))
      await controller.poll()

      controller.stop()
      answer.resolve(
        problemResponse(
          'provider_unavailable',
          'Unavailable.',
          providerStatus({ code: 'provider_unavailable', last_attempt: '2026-10-13T12:00:00Z' }),
        ),
      )
      await refreshing
      await settle()

      expect(notices()).toEqual({ banners: [], status: '' })
    })

    it('shows nothing from a status check that answers after it stopped', async () => {
      const current = testProjection({ capabilities: [capability(true)] })
      const host = hostStub(current)
      const transport = transportStub({ status: providerStatus({ refreshing: true }) })
      const controller = createProviderController({ transport, host, autoRefresh: false })
      controller.sync(current)
      await controller.poll()

      // The refresh it follows commits while the next status check is on its way.
      const answer = Promise.withResolvers<ProviderStatus>()
      transport.status.mockReturnValueOnce(answer.promise)
      const polled = controller.poll()
      controller.stop()
      answer.resolve(
        providerStatus({
          generation: '2',
          summary: { ...emptySummary(), imported_transactions: 7 },
        }),
      )
      await polled
      await settle()

      expect({ reread: host.reload.mock.calls.length, notices: notices() }).toEqual({
        reread: 0,
        notices: { banners: [], status: '' },
      })
    })

    it('rereads nothing from the status check after an expired confirmation', async () => {
      const current = testProjection({ capabilities: [capability(true)] })
      const host = hostStub(current)
      const transport = transportStub({
        mutationResponse: problemResponse(
          'provider_deletion_confirmation_required',
          'Confirm provider removals.',
          providerStatus({
            code: 'provider_deletion_confirmation_required',
            confirmation_token: 'opaque-confirmation',
          }),
        ),
      })
      const controller = createProviderController({ transport, host })
      controller.sync(current)
      await controller.refresh()
      transport.mutations.request.mockResolvedValueOnce(
        problemResponse('provider_confirmation_invalid', 'Confirmation expired.'),
      )
      const statusRead = Promise.withResolvers<ProviderStatus>()
      transport.status.mockReturnValueOnce(statusRead.promise)
      const confirmation = controller.confirm()
      await settle()

      controller.stop()
      // Another refresh saved newer data meanwhile, which is why the confirmation expired.
      statusRead.resolve(providerStatus({ generation: '2' }))
      await confirmation
      await settle()

      expect(host.reload).not.toHaveBeenCalled()
    })
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
  const shown = vi.fn(() => current)
  return {
    current: shown,
    accept: vi.fn((projection: ViewProjection) => shown.mockReturnValue(projection)),
    reload: vi.fn(async () => undefined),
    serially: <T>(work: () => Promise<T>) => work(),
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
    revision: String(BigInt(overrides.generation ?? '1') - 1n),
    generation: '1',
    refresh_due: 'none',
    manual_refresh: 'full',
    refreshing: false,
    progress: { fetched: 0, total: 0, verifying: false, percent: 0 },
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

function problemResponse(
  code: string,
  detail: string,
  status?: ProviderStatus,
  httpStatus = 409,
): Response {
  const problem = {
    type: 'about:blank',
    title: 'Request failed',
    status: httpStatus,
    detail,
    code,
    ...(status === undefined ? {} : { provider: status }),
  }
  return Response.json(problem, {
    status: httpStatus,
    headers: { 'Content-Type': 'application/problem+json' },
  })
}

/** Lets pending promise callbacks run. */
function settle(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve))
}
