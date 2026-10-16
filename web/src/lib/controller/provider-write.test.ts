import { describe, expect, it, vi } from 'vitest'

import type { ProviderWriteResponse, ProviderWriteStatus, ViewProjection } from '../api/client'
import { notices } from '../../test/notices'
import { testProjection } from '../../test/projection'
import { createProviderWriteController } from './provider-write'

describe('provider write controller', () => {
  it('keeps recorded completion counts and its announcement across repeated polls', async () => {
    const transport = transportStub({
      status: writeStatus({
        completed_at: '2026-10-15T12:00:00Z',
        total: 2,
        completed: 2,
        overrides: 1,
      }),
    })
    const host = hostStub()
    const controller = createProviderWriteController({ transport, host })
    controller.install(writeStatus({ phase: 'writing', total: 2, completed: 1, remaining: 1 }))
    await controller.poll()
    await controller.poll()
    expect(controller.state.phase).toBe('complete')
    expect(controller.state.announcement).toBe('Provider write complete.')
    expect(controller.state.status).toMatchObject({
      total: 2,
      completed: 2,
      remaining: 0,
      overrides: 1,
    })
    expect(host.reload).toHaveBeenCalledOnce()
  })

  it('shows a recorded completion after the profile reopens', () => {
    const controller = createProviderWriteController({
      transport: transportStub(),
      host: hostStub(),
    })
    controller.install(
      writeStatus({ completed_at: '2026-10-15T12:00:00Z', total: 3, completed: 3 }),
    )
    expect(controller.state.phase).toBe('complete')
    expect(controller.state.announcement).toBe('Provider write complete.')
  })

  it('reports discarded edits after reconciliation instead of successful writes', async () => {
    const controller = createProviderWriteController({
      transport: transportStub(),
      host: hostStub(),
    })
    controller.install(writeStatus({ phase: 'attention_required', total: 2, batch_version: '4' }))
    await controller.reconcile()
    await controller.poll()
    expect(controller.state.phase).toBe('complete')
    expect(controller.state.announcement).toBe('Provider write stopped. Remaining edits discarded.')
  })

  it('keeps completion audit warnings after refreshing and polling again', async () => {
    const warning = 'The audit log could not record completion.'
    const transport = transportStub({
      status: writeStatus({ audit_warning: warning, completed_at: '2026-10-15T12:00:00Z' }),
    })
    const host = hostStub()
    const controller = createProviderWriteController({ transport, host })
    controller.install(writeStatus({ phase: 'writing', batch_version: '4' }))

    await controller.poll()

    expect(host.reload).toHaveBeenCalledOnce()
    expect(controller.state.phase).toBe('complete')
    expect(controller.state.announcement).toBe(`Provider write complete. ${warning}`)
    await controller.poll()
    expect(controller.state.announcement).toContain(warning)
  })

  it.each(['reconciling', 'reconnect_required'])(
    'waits for explicit Monarch recovery after %s',
    async (phase) => {
      const transport = transportStub({
        status: writeStatus({ phase, batch_version: '4', actions: ['reconcile'] }),
      })
      const controller = createProviderWriteController({
        transport,
        host: hostStub(testProjection({ profile_kind: 'monarch' })),
      })
      await controller.poll()
      expect(transport.mutations.request).not.toHaveBeenCalled()
      expect(controller.can('reconcile')).toBe(true)
      await controller.reconcile()
      expect(transport.mutations.request).toHaveBeenCalledOnce()
      expect(vi.mocked(transport.mutations.request).mock.calls[0]![0]).toBe(
        'api/v1/provider/write/reconcile',
      )
    },
  )
  it('names YNAB in write and reconnect announcements', () => {
    const controller = createProviderWriteController({
      transport: transportStub(),
      host: hostStub(testProjection({ profile_kind: 'ynab' })),
    })
    controller.install(writeStatus({ phase: 'writing' }))
    expect(controller.state.announcement).toContain('YNAB')
    controller.install(writeStatus({ phase: 'reconnect_required' }))
    expect(controller.state.announcement).toContain('YNAB')
    expect(controller.state.announcement).not.toContain('Monarch')
  })
  it('does not poll or resume while the document is hidden', async () => {
    const transport = transportStub({
      status: writeStatus({ phase: 'writing', actions: ['resume'] }),
    })
    const controller = createProviderWriteController({
      transport,
      host: hostStub(),
      visible: () => false,
    })

    await controller.poll()

    expect(transport.status).not.toHaveBeenCalled()
    expect(transport.mutations.request).not.toHaveBeenCalled()
  })

  it('automatically resumes only ownerless running work and reloads on completion', async () => {
    const completed = writeStatus()
    const transport = transportStub({
      status: writeStatus({ phase: 'writing', batch_version: '4', actions: ['resume'] }),
      mutationResponse: Response.json({ status: completed } satisfies ProviderWriteResponse),
    })
    const host = hostStub()
    const controller = createProviderWriteController({ transport, host })

    await controller.poll()

    expect(transport.mutations.request).toHaveBeenCalledWith(
      'api/v1/provider/write/resume',
      JSON.stringify({ version: '1', expected_batch_version: '4' }),
      undefined,
    )
    expect(host.reload).toHaveBeenCalledTimes(1)
    expect(controller.state.phase).toBe('complete')
  })

  it('keeps paused and reconcile-only states explicit', async () => {
    const transport = transportStub({
      status: writeStatus({
        phase: 'attention_required',
        reason: 'provider_write_target_not_found',
        batch_version: '8',
        actions: ['reconcile'],
      }),
    })
    const controller = createProviderWriteController({ transport, host: hostStub() })

    await controller.poll()

    expect(controller.state.phase).toBe('attention')
    expect(transport.mutations.request).not.toHaveBeenCalled()
    expect(controller.can('resume')).toBe(false)
    expect(controller.can('reconcile')).toBe(true)
  })

  it('uses exact batch versions and accepts authoritative reconcile projections', async () => {
    const current = testProjection({ canonical_query: 'v=1&group=category' })
    const next = testProjection({
      canonical_query: current.canonical_query,
      revision: '9',
      selection: 'mfsel1.next' as ViewProjection['selection'],
    })
    const response: ProviderWriteResponse = {
      status: writeStatus(),
      projection: next,
      selection: { kind: 'cleared', value: next.selection },
    }
    const transport = transportStub({ mutationResponse: Response.json(response) })
    const host = hostStub(current)
    const controller = createProviderWriteController({ transport, host })
    controller.install(
      writeStatus({ phase: 'paused', batch_version: '7', actions: ['resume', 'reconcile'] }),
    )

    await expect(controller.reconcile()).resolves.toBe(true)

    expect(host.accept).toHaveBeenCalledWith(next)
    const [, raw] = vi.mocked(transport.mutations.request).mock.calls[0]!
    expect(JSON.parse(raw)).toMatchObject({
      expected_batch_version: '7',
      query: current.canonical_query,
      selection: current.selection,
    })
  })

  it('requires explicit confirmation and never replays an invalid token', async () => {
    const pending = writeStatus({
      phase: 'reconcile_confirmation_required',
      batch_version: '10',
      actions: ['confirm'],
    })
    const transport = transportStub({
      mutationResponse: problemResponse(
        'provider_deletion_confirmation_required',
        pending,
        'opaque-confirmation',
      ),
      status: pending,
    })
    const controller = createProviderWriteController({ transport, host: hostStub() })
    controller.install(
      writeStatus({ phase: 'attention_required', batch_version: '9', actions: ['reconcile'] }),
    )

    await expect(controller.reconcile()).resolves.toBe(false)
    expect(controller.state.phase).toBe('confirmation')
    await controller.poll()
    expect(controller.can('confirm')).toBe(true)

    vi.mocked(transport.mutations.request).mockResolvedValueOnce(
      problemResponse('provider_confirmation_invalid', pending),
    )
    await expect(controller.confirm()).resolves.toBe(false)
    expect(transport.mutations.request).toHaveBeenCalledTimes(2)
    expect(controller.state.phase).toBe('failed')
  })

  it('refetches a confirmation candidate after a browser restart loses its token', async () => {
    const pending = writeStatus({
      phase: 'reconcile_confirmation_required',
      batch_version: '10',
      actions: ['confirm'],
    })
    const transport = transportStub({
      mutationResponse: problemResponse(
        'provider_deletion_confirmation_required',
        pending,
        'new-confirmation',
      ),
    })
    const controller = createProviderWriteController({ transport, host: hostStub() })
    controller.install(pending)

    expect(controller.can('confirm')).toBe(false)
    expect(controller.can('reconcile')).toBe(true)
    await expect(controller.reconcile()).resolves.toBe(false)
    expect(controller.can('confirm')).toBe(true)
  })

  it('confirms with the token of a parked reconcile it did not start', async () => {
    const parked = writeStatus({
      phase: 'reconcile_confirmation_required',
      batch_version: '12',
      actions: ['confirm'],
    })
    const transport = transportStub({
      mutationResponse: problemResponse(
        'provider_deletion_confirmation_required',
        parked,
        'parked-attempt-token',
      ),
    })
    const controller = createProviderWriteController({ transport, host: hostStub() })
    controller.install(
      writeStatus({ phase: 'attention_required', batch_version: '11', actions: ['reconcile'] }),
    )

    await expect(controller.reconcile()).resolves.toBe(false)
    expect(controller.state.phase).toBe('confirmation')
    expect(controller.state.status).toEqual(parked)
    vi.mocked(transport.mutations.request).mockResolvedValueOnce(
      Response.json({ status: writeStatus() } satisfies ProviderWriteResponse),
    )
    await expect(controller.confirm()).resolves.toBe(true)

    const [path, raw] = vi.mocked(transport.mutations.request).mock.calls[1]!
    expect(path).toBe('api/v1/provider/write/reconcile/confirm')
    expect(JSON.parse(raw)).toMatchObject({
      expected_batch_version: '12',
      confirmation_token: 'parked-attempt-token',
    })
  })

  it('keeps its phase and polls at once when a 503 says the write runs on', async () => {
    const transport = transportStub({
      mutationResponse: Response.json(
        {
          type: 'about:blank',
          title: 'Service Unavailable',
          status: 503,
          detail: 'The provider write is still running.',
          code: 'provider_write_in_progress',
        },
        { status: 503, headers: { 'Content-Type': 'application/problem+json' } },
      ),
    })
    const polled = Promise.withResolvers<void>()
    transport.status.mockImplementationOnce(() => {
      polled.resolve()
      return new Promise<ProviderWriteStatus>(() => undefined)
    })
    const controller = createProviderWriteController({ transport, host: hostStub() })
    controller.install(
      writeStatus({ phase: 'paused', batch_version: '7', actions: ['resume', 'reconcile'] }),
    )

    await expect(controller.resume()).resolves.toBe(false)

    expect(controller.state.phase).toBe('paused')
    await polled.promise
  })
})

describe('provider write failures the user must see', () => {
  const flashes = () => notices().banners

  it('shows why a background write stopped while the batch still reads as writing', async () => {
    const stopped = writeStatus({
      phase: 'writing',
      batch_version: '4',
      total: 2,
      run_failure: { code: 'store_busy', detail: 'The profile is busy. Try again.' },
    })
    const controller = createProviderWriteController({
      transport: transportStub({ status: stopped }),
      host: hostStub(),
    })

    await controller.poll()
    await controller.poll()

    const message =
      'Provider write stopped: The profile is busy. Try again. ' +
      'Moneyflow resumes it within 2 minutes.'
    expect(controller.state.announcement).toBe(message)
    expect(flashes()).toEqual([{ message, tone: 'danger' }])
  })

  it('reports a completed write whose view reload failed as saved', async () => {
    const host = hostStub()
    host.reload.mockRejectedValueOnce(new TypeError('Failed to fetch'))
    const completed = writeStatus({ completed_at: '2026-10-15T12:00:00Z', total: 1, completed: 1 })
    const controller = createProviderWriteController({
      transport: transportStub({ status: completed }),
      host,
    })
    controller.install(writeStatus({ phase: 'writing', batch_version: '4', total: 1 }))

    await controller.poll()

    expect(controller.state.phase).toBe('complete')
    expect(controller.state.announcement).toBe(
      'Provider write complete. The view could not be refreshed; reload the page to see it.',
    )
    expect(notices()).toEqual({
      banners: [],
      status:
        'Saved 1 change to Monarch. The view could not be refreshed; reload the page to see it.',
    })
  })

  it('shows a finished write in the status bar, with any audit warning', async () => {
    const warning = 'The audit log could not record completion.'
    const completed = writeStatus({
      completed_at: '2026-10-15T12:00:00Z',
      total: 6,
      completed: 6,
      audit_warning: warning,
    })
    const controller = createProviderWriteController({
      transport: transportStub({ status: completed }),
      host: hostStub(testProjection({ profile_kind: 'ynab' })),
    })
    controller.install(writeStatus({ phase: 'writing', batch_version: '4', total: 6 }))

    await controller.poll()

    expect(notices()).toEqual({ banners: [], status: `Saved 6 changes to YNAB. ${warning}` })
  })

  it('shows a write that ended without finishing in a failure banner', async () => {
    const controller = createProviderWriteController({
      transport: transportStub(),
      host: hostStub(),
    })
    controller.install(writeStatus({ phase: 'writing', batch_version: '4', total: 2 }))

    await controller.poll()

    expect(notices()).toEqual({
      banners: [{ message: 'Provider write ended.', tone: 'danger' }],
      status: '',
    })
  })

  it('shows a finished write as saved after a later revision cleared its completion time', async () => {
    // Revision 7 prepared the write, which committed 8; another client's edit then made 9.
    const controller = createProviderWriteController({
      transport: transportStub({ status: writeStatus({ revision: '9', completed_revision: '8' }) }),
      host: hostStub(),
    })
    controller.install(
      writeStatus({
        phase: 'writing',
        batch_version: '4',
        revision: '7',
        prepared_revision: '7',
        total: 2,
      }),
    )

    await controller.poll()

    expect(controller.state.phase).toBe('complete')
    expect(controller.state.announcement).toBe('Provider write complete.')
    expect(notices()).toEqual({ banners: [], status: 'Saved 2 changes to Monarch.' })
  })

  // The server reads the batch and the revision separately, so a poll can see the batch still
  // running at the revision its finalization just committed. Another tab's edit then clears the
  // completion time before the next poll.
  it('shows a write as saved when its last running status already carried its commit', async () => {
    const controller = createProviderWriteController({
      transport: transportStub({ status: writeStatus({ revision: '9', completed_revision: '8' }) }),
      host: hostStub(),
    })
    controller.install(
      writeStatus({
        phase: 'writing',
        batch_version: '4',
        revision: '8',
        prepared_revision: '7',
        total: 2,
      }),
    )

    await controller.poll()

    expect(controller.state.announcement).toBe('Provider write complete.')
    expect(notices()).toEqual({ banners: [], status: 'Saved 2 changes to Monarch.' })
  })

  // The write finishes and another tab's edit clears its completion time before the next poll, so
  // Pause answers with the finished batch.
  it('shows a write as saved when a control request finds it already finished', async () => {
    const finished = writeStatus({ revision: '9', completed_revision: '8' })
    const controller = createProviderWriteController({
      transport: transportStub({ mutationResponse: Response.json({ status: finished }) }),
      host: hostStub(),
    })
    controller.install(
      writeStatus({
        phase: 'writing',
        batch_version: '4',
        revision: '7',
        prepared_revision: '7',
        total: 2,
      }),
    )

    await expect(controller.pause()).resolves.toBe(true)

    expect(controller.state.announcement).toBe('Provider write complete.')
    expect(notices()).toEqual({ banners: [], status: 'Saved 2 changes to Monarch.' })
  })

  it('shows an ended write as a failure when the last completed write was an earlier one', async () => {
    const controller = createProviderWriteController({
      transport: transportStub({ status: writeStatus({ revision: '9', completed_revision: '5' }) }),
      host: hostStub(),
    })
    controller.install(
      writeStatus({
        phase: 'writing',
        batch_version: '4',
        revision: '7',
        prepared_revision: '7',
        total: 2,
      }),
    )

    await controller.poll()

    expect(notices()).toEqual({
      banners: [{ message: 'Provider write ended.', tone: 'danger' }],
      status: '',
    })
  })

  it('shows a write reconciled before it finished in a failure banner', async () => {
    const controller = createProviderWriteController({
      transport: transportStub(),
      host: hostStub(),
    })
    controller.install(writeStatus({ phase: 'attention_required', total: 2, batch_version: '4' }))

    await controller.reconcile()

    expect(notices()).toEqual({
      banners: [{ message: 'Provider write stopped. Remaining edits discarded.', tone: 'danger' }],
      status: '',
    })
  })

  it('rereads a view that moved on while the server reconciled, without reporting failure', async () => {
    const merchant = testProjection({ canonical_query: 'v=1&group=merchant' })
    const category = testProjection({ canonical_query: 'v=1&group=category' })
    const reread = testProjection({ canonical_query: 'v=1&group=category', revision: '9' })
    const response: ProviderWriteResponse = {
      status: writeStatus(),
      projection: testProjection({ canonical_query: 'v=1&group=merchant', revision: '9' }),
      selection: { kind: 'cleared', value: merchant.selection },
    }
    let shown = merchant
    const accept = vi.fn(() => true)
    const host = {
      current: vi.fn(() => shown),
      accept,
      claim: () => accept,
      reload: vi.fn(async () => reread),
    }
    let answer!: (response: Response) => void
    const transport = transportStub()
    transport.mutations.request.mockImplementationOnce(
      () => new Promise<Response>((resolve) => (answer = resolve)),
    )
    const controller = createProviderWriteController({ transport, host })
    controller.install(
      writeStatus({ phase: 'paused', batch_version: '7', actions: ['resume', 'reconcile'] }),
    )

    const reconciling = controller.reconcile()
    await vi.waitFor(() => expect(transport.mutations.request).toHaveBeenCalledOnce())
    const [, rawBody] = transport.mutations.request.mock.calls[0]!
    expect(JSON.parse(rawBody)).toMatchObject({ query: 'v=1&group=merchant' })
    shown = category
    answer(Response.json(response))

    await expect(reconciling).resolves.toBe(true)
    expect(controller.state.phase).toBe('complete')
    expect(host.reload).toHaveBeenCalledOnce()
    expect(host.accept).not.toHaveBeenCalled()
  })

  it('shows a failed status poll once, without the write drawer open', async () => {
    const transport = transportStub()
    transport.status
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
    const controller = createProviderWriteController({ transport, host: hostStub() })
    controller.install(writeStatus({ phase: 'writing', batch_version: '4' }))

    await controller.poll()
    await controller.poll()

    expect(controller.state.phase).toBe('failed')
    expect(flashes()).toEqual([
      {
        message: 'Provider write status is unavailable, so the progress shown may be out of date.',
        tone: 'danger',
      },
    ])
  })
})

describe('a stopped provider write controller', () => {
  const finished = () =>
    writeStatus({ completed_at: '2026-10-15T12:00:00Z', total: 2, completed: 2 })

  it('rereads and reports nothing from a status check that answers after it stopped', async () => {
    const host = hostStub()
    const transport = transportStub()
    const answer = Promise.withResolvers<ProviderWriteStatus>()
    transport.status.mockReturnValueOnce(answer.promise)
    const controller = createProviderWriteController({ transport, host })
    controller.install(writeStatus({ phase: 'writing', batch_version: '4', total: 2 }))

    const polled = controller.poll()
    controller.stop()
    // The write finished while the status check was on its way.
    answer.resolve(finished())
    await polled

    expect({ reread: host.reload.mock.calls.length, notices: notices() }).toEqual({
      reread: 0,
      notices: { banners: [], status: '' },
    })
  })

  it('rereads and reports nothing from a write request that answers after it stopped', async () => {
    const host = hostStub()
    const transport = transportStub()
    const answer = Promise.withResolvers<Response>()
    transport.mutations.request.mockReturnValueOnce(answer.promise)
    const controller = createProviderWriteController({ transport, host })
    controller.install(
      writeStatus({ phase: 'paused', batch_version: '4', total: 2, actions: ['resume'] }),
    )

    const resumed = controller.resume()
    controller.stop()
    answer.resolve(Response.json({ status: finished() }))
    await resumed

    expect({ reread: host.reload.mock.calls.length, notices: notices() }).toEqual({
      reread: 0,
      notices: { banners: [], status: '' },
    })
  })

  it.each<[string, (answer: PromiseWithResolvers<ProviderWriteStatus>) => void]>([
    ['fails', (answer) => answer.reject(new TypeError('Failed to fetch'))],
    [
      'reports a stopped run',
      (answer) =>
        answer.resolve(
          writeStatus({
            phase: 'writing',
            batch_version: '4',
            total: 2,
            run_failure: { code: 'store_busy', detail: 'The profile is busy. Try again.' },
          }),
        ),
    ],
  ])('shows no failure from a status check that %s after it stopped', async (_, settle) => {
    const transport = transportStub()
    const answer = Promise.withResolvers<ProviderWriteStatus>()
    transport.status.mockReturnValueOnce(answer.promise)
    const controller = createProviderWriteController({ transport, host: hostStub() })
    controller.install(writeStatus({ phase: 'writing', batch_version: '4', total: 2 }))

    const polled = controller.poll()
    controller.stop()
    settle(answer)
    await polled

    expect(notices()).toEqual({ banners: [], status: '' })
  })
})

function hostStub(current = testProjection()) {
  const accept = vi.fn<(projection: ViewProjection) => boolean>(() => true)
  return {
    current: vi.fn(() => current),
    /** The view-showing function every write request's claim returns. */
    accept,
    claim: () => accept,
    reload: vi.fn(async () => current),
  }
}

function transportStub(
  options: { status?: ProviderWriteStatus; mutationResponse?: Response } = {},
) {
  return {
    mutations: {
      request: vi.fn(async (_path: string, _body: string, _signal?: AbortSignal) => {
        void _path
        void _body
        void _signal
        return (options.mutationResponse ?? Response.json({ status: writeStatus() })).clone()
      }),
    },
    status: vi.fn(async () => options.status ?? writeStatus()),
  }
}

function writeStatus(overrides: Partial<ProviderWriteStatus> = {}): ProviderWriteStatus {
  return {
    version: '1',
    revision: '1',
    generation: '1',
    total: 0,
    completed: 0,
    failed: 0,
    remaining: 0,
    overrides: 0,
    actions: [],
    ...overrides,
  }
}

function problemResponse(
  code: string,
  status: ProviderWriteStatus,
  confirmationToken?: string,
): Response {
  return Response.json(
    {
      type: 'about:blank',
      title: 'Conflict',
      status: 409,
      detail: 'The provider write requires attention.',
      code,
      provider_write: status,
      ...(confirmationToken ? { provider_write_confirmation_token: confirmationToken } : {}),
    },
    { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
  )
}
