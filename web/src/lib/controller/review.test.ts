import { describe, expect, it, vi } from 'vitest'

import type { MutationFetch } from '../api/client'
import { createReviewController } from './review'

describe('bounded review controller', () => {
  it('captures the reviewed revision and keeps active and redo operations separate', async () => {
    const transport = responseTransport(reviewResponse('12'))
    const controller = createReviewController({ transport, revision: () => 12n })

    await expect(controller.load()).resolves.toBe(true)

    expect(controller.state.reviewedRevision).toBe(12n)
    expect(controller.state.activeOperations.map((operation) => operation.operation_id)).toEqual([
      'active-1',
    ])
    expect(controller.state.inactiveOperations.map((operation) => operation.operation_id)).toEqual([
      'redo-1',
    ])
    const [, body] = vi.mocked(transport.request).mock.calls[0]!
    expect(JSON.parse(body)).toEqual({ version: '1', expected_revision: '12' })
  })

  it('loads an operation’s first rows, then the rest in bounded requests', async () => {
    const transport = targetServer(901)
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()
    expect(transport.request).toHaveBeenCalledTimes(1)

    await expect(controller.loadTargets('active-1', 20)).resolves.toBe(true)
    expect(controller.targets('active-1')).toHaveLength(20)
    await expect(controller.loadTargets('active-1', 901)).resolves.toBe(true)

    const rows = controller.targets('active-1')
    expect(rows).toHaveLength(901)
    expect(rows[0]?.merchant).toBe('Example Merchant 1')
    expect(rows[900]?.merchant).toBe('Example Merchant 901')
    const windows = vi
      .mocked(transport.request)
      .mock.calls.slice(1)
      .map(([, body]) => (JSON.parse(body) as { window: unknown }).window)
    expect(windows).toEqual([
      { offset: 0, limit: 20 },
      { offset: 20, limit: 400 },
      { offset: 420, limit: 400 },
      { offset: 820, limit: 81 },
    ])
    await controller.loadTargets('active-1', 901)
    expect(transport.request).toHaveBeenCalledTimes(5)
  })

  it('keeps each operation’s rows and reports which operation is loading', async () => {
    let release = (): void => undefined
    const transport = targetServer(30, () => new Promise<void>((resolve) => (release = resolve)))
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()

    const first = controller.loadTargets('active-1', 20)
    expect(controller.targetsLoading('active-1')).toBe(true)
    expect(controller.targetsLoading('redo-1')).toBe(false)
    await vi.waitFor(() => expect(transport.request).toHaveBeenCalledTimes(2))
    release()
    await first
    expect(controller.targetsLoading('active-1')).toBe(false)
    const second = controller.loadTargets('redo-1', 20)
    await vi.waitFor(() => expect(transport.request).toHaveBeenCalledTimes(3))
    release()
    await second

    expect(controller.targets('active-1')).toHaveLength(20)
    expect(controller.targets('redo-1')).toHaveLength(20)
  })

  it('drops rows that arrive after the review reloads', async () => {
    let release = (): void => undefined
    const transport = targetServer(30, () => new Promise<void>((resolve) => (release = resolve)))
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()
    const stale = controller.loadTargets('active-1', 20)
    await vi.waitFor(() => expect(transport.request).toHaveBeenCalledTimes(2))
    const reload = controller.load()
    release()
    await expect(stale).resolves.toBe(false)
    await reload

    expect(controller.targets('active-1')).toEqual([])
    expect(controller.targetsLoading('active-1')).toBe(false)
  })

  it('keeps the rows it loaded when a later request fails', async () => {
    const transport = targetServer(500)
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()
    await controller.loadTargets('active-1', 20)
    vi.mocked(transport.request).mockRejectedValueOnce(new Error('offline'))

    await expect(controller.loadTargets('active-1', 500)).resolves.toBe(false)

    expect(controller.targets('active-1')).toHaveLength(20)
    expect(controller.targetsLoading('active-1')).toBe(false)
    expect(controller.state.announcement).toBe('Pending changes could not be loaded.')

    // Trying again clears the failure.
    await expect(controller.loadTargets('active-1', 500)).resolves.toBe(true)
    expect(controller.targets('active-1')).toHaveLength(500)
    expect(controller.state.announcement).toBe('')
  })

  it('keeps each operation’s targets when an older request answers last', async () => {
    const answers: Array<(response: Response) => void> = []
    const transport: MutationFetch = {
      request: vi.fn(async (path: string) =>
        path.endsWith('/targets')
          ? await new Promise<Response>((resolve) => answers.push(resolve))
          : jsonResponse(reviewResponse('7')),
      ),
    }
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()

    const first = controller.loadTargets('A', 1)
    const second = controller.loadTargets('B', 1)
    await vi.waitFor(() => expect(answers).toHaveLength(2))
    const merchant = (name: string) => {
      const response = reviewResponse('7')
      response.targets[0]!.merchant = name
      return jsonResponse(response)
    }
    answers[1]!(merchant('Merchant B'))
    await expect(second).resolves.toBe(true)
    answers[0]!(merchant('Merchant A'))
    await expect(first).resolves.toBe(true)

    expect(controller.targets('B').map((target) => target.merchant)).toEqual(['Merchant B'])
    expect(controller.targets('A').map((target) => target.merchant)).toEqual(['Merchant A'])
    expect(controller.state.phase).toBe('idle')
  })

  it('does not replace the captured reviewed revision when the live revision advances', async () => {
    let liveRevision = 4n
    const transport = responseTransport(reviewResponse('4'))
    const controller = createReviewController({ transport, revision: () => liveRevision })
    await controller.load()

    liveRevision = 5n

    expect(controller.state.reviewedRevision).toBe(4n)
    expect(controller.isStale()).toBe(true)
  })
})

/** A review server whose operations each affect `affected` rows, named by row number. */
function targetServer(affected: number, wait?: () => Promise<void>): MutationFetch {
  return {
    request: vi.fn(async (path: string, body: string) => {
      if (!path.endsWith('/targets')) return jsonResponse(reviewResponse('7'))
      await wait?.()
      const { offset, limit } = (JSON.parse(body) as { window: { offset: number; limit: number } })
        .window
      const count = Math.max(0, Math.min(limit, affected - offset))
      const targets = Array.from({ length: count }, (_, index) => ({
        ...reviewResponse('7').targets[0],
        merchant: `Example Merchant ${offset + index + 1}`,
      }))
      return jsonResponse({ ...reviewResponse('7'), window: { offset, limit, count }, targets })
    }),
  }
}

function responseTransport(body: unknown): MutationFetch {
  return { request: vi.fn(async () => jsonResponse(body)) }
}

function reviewResponse(revision: string, offset = 0, limit = 100) {
  return {
    version: '1',
    revision,
    pending: { active_operations: 1, inactive_operations: 1, affected_transactions: 2 },
    active_operations: [
      { operation_id: 'active-1', type: 'merchant.rename', active: true, affected_count: 1 },
    ],
    inactive_operations: [
      { operation_id: 'redo-1', type: 'hide.toggle', active: false, affected_count: 1 },
    ],
    operation_id: offset === 0 && limit === 100 ? undefined : 'active-1',
    window: { offset, limit, count: 1 },
    targets: [
      {
        date: '2024-01-01',
        merchant: 'Example Merchant',
        category: 'Category',
        amount: {
          minor: '-1234',
          currency: 'USD',
          scale: 2,
          decimal: '-12.34',
          display: '-12.34',
        },
        hidden: false,
      },
    ],
  }
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}
