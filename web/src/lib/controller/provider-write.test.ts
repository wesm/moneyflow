import { describe, expect, it, vi } from 'vitest'

import type { ProviderWriteResponse, ProviderWriteStatus, ViewProjection } from '../api/client'
import { testProjection } from '../../test/projection'
import { createProviderWriteController } from './provider-write'

describe('provider write controller', () => {
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
