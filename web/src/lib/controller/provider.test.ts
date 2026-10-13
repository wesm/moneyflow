import { dismissFlash, getFlashes } from '@kenn-io/kit-ui'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ProviderStatus, ViewProjection } from '../api/client'
import { createProviderController, REFRESH_CANCELED, REFRESH_NOT_CONFIRMED } from './provider'
import { staleNotice } from './refresh-notice'
import { testProjection } from '../../test/projection'

describe('provider controller', () => {
  afterEach(() => dismissFlash())

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
    expect(getFlashes().map(({ message, tone }) => ({ message, tone }))).toEqual([
      { message: 'Refresh complete: 0 imported, 0 removed.', tone: 'success' },
      { message: warning, tone: 'warning' },
    ])
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

  it('says so when the view changed during a refresh and cannot be reread', async () => {
    const sent = testProjection({ canonical_query: 'v=1', capabilities: [capability(true)] })
    const host = hostStub(sent)
    host.reload.mockRejectedValue(new Error('offline'))
    const transport = transportStub({
      mutationResponse: Response.json(refreshResponse(sent, 'preserved')),
    })
    vi.mocked(transport.mutations.request).mockImplementation(async () => {
      host.current.mockReturnValue(testProjection({ canonical_query: 'group=category&v=1' }))
      return Response.json(refreshResponse(sent, 'preserved'))
    })
    const controller = createProviderController({ transport, host })
    controller.sync(sent)

    await expect(controller.refresh()).resolves.toBe(true)

    expect(host.accept).not.toHaveBeenCalled()
    expect(controller.state.announcement).toBe(
      'Refresh complete: 0 imported, 0 removed. The current view could not be reread.',
    )
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
    expect(getFlashes()).toEqual([])
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
    expect(controller.state.staleReason).toBe(REFRESH_CANCELED)
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
    const controller = createProviderController({ transport, host: hostStub(current) })
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
    const controller = createProviderController({ transport, host: hostStub() })
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
    expect(controller.state.staleReason).toBe(REFRESH_CANCELED)

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
    await controller.poll()
    expect(transport.mutations.request).not.toHaveBeenCalled()

    await expect(controller.refresh()).resolves.toBe(true)
    expect(JSON.parse(vi.mocked(transport.mutations.request).mock.calls[0]![1]).manual).toBe(true)
  })

  it('cancels a refresh still waiting for the first view without sending it', async () => {
    const host = hostStub()
    host.current.mockReturnValue(undefined as unknown as ViewProjection)
    const transport = transportStub({
      status: providerStatus({ provider_kind: 'monarch', refresh_due: 'recent' }),
    })
    const controller = createProviderController({ transport, host })

    await controller.poll()
    expect(controller.state.dialog?.view).toBe('progress')
    controller.cancel()
    controller.sync(testProjection({ capabilities: [capability(true)] }))

    expect(transport.mutations.request).not.toHaveBeenCalled()
    expect(controller.state.staleReason).toBe(REFRESH_CANCELED)
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
    expect(getFlashes()).toEqual([])

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

  it('follows another interface refresh without starting a competing one', async () => {
    const current = testProjection({ capabilities: [capability(true)] })
    const host = hostStub(current)
    const transport = transportStub({
      status: providerStatus({
        refreshing: true,
        owner_renderer: 'tui',
        refresh_due: 'full',
        progress: { fetched: 20, total: 40 },
      }),
    })
    const controller = createProviderController({ transport, host })
    controller.sync(current)

    await controller.poll()

    expect(controller.state.phase).toBe('refreshing')
    expect(controller.state.dialog?.view).toBe('other')
    expect(controller.state.announcement).toBe(
      'The terminal app (moneyflow tui) is refreshing this profile.',
    )
    expect(transport.mutations.request).not.toHaveBeenCalled()

    vi.mocked(transport.status).mockResolvedValueOnce(
      providerStatus({ generation: '2', summary: { ...emptySummary(), imported_transactions: 7 } }),
    )
    await controller.poll()
    await vi.waitFor(() => expect(controller.state.phase).toBe('idle'))
    expect(controller.state.dialog).toBeUndefined()
    expect(host.reload).toHaveBeenCalledOnce()
    expect(controller.state.announcement).toBe('Refresh complete: 7 imported, 0 removed.')
  })

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

  it('closes the dialog when another interface refresh stops without new data', async () => {
    const transport = transportStub({ status: providerStatus({ refreshing: true }) })
    const controller = createProviderController({ transport, host: hostStub() })
    await controller.poll()
    vi.mocked(transport.status).mockResolvedValueOnce(
      providerStatus({ code: 'provider_unavailable', last_success: '2026-10-03T12:00:00Z' }),
    )

    await controller.poll()

    expect(controller.state.dialog).toBeUndefined()
    expect(controller.state.phase).toBe('failed')
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
    revision: '1',
    generation: '1',
    refresh_due: 'none',
    refreshing: false,
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
