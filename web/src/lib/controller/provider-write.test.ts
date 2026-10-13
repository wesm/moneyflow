import { dismissFlash, getFlashes } from '@kenn-io/kit-ui'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ProviderWriteResponse, ProviderWriteStatus, ViewProjection } from '../api/client'
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
})

describe('provider write failures the user must see', () => {
  afterEach(() => dismissFlash())
  const flashes = () => getFlashes().map(({ message, tone }) => ({ message, tone }))

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

  it('reports a completed write whose view reload failed as complete', async () => {
    const host = hostStub()
    host.reload.mockRejectedValueOnce(new TypeError('Failed to fetch'))
    const controller = createProviderWriteController({
      transport: transportStub({ status: writeStatus({ completed_at: '2026-10-15T12:00:00Z' }) }),
      host,
    })
    controller.install(writeStatus({ phase: 'writing', batch_version: '4' }))

    await controller.poll()

    expect(controller.state.phase).toBe('complete')
    expect(controller.state.announcement).toBe(
      'Provider write complete. The view could not be refreshed; reload the page to see it.',
    )
    expect(flashes()).toEqual([
      {
        message:
          'Provider write complete. The view could not be refreshed; reload the page to see it.',
        tone: 'warning',
      },
    ])
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
    const host = {
      current: vi.fn(() => shown),
      accept: vi.fn(),
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
    expect(host.accept.mock.calls).toEqual([[reread]])
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

function hostStub(current = testProjection()) {
  return {
    current: vi.fn(() => current),
    accept: vi.fn(),
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
