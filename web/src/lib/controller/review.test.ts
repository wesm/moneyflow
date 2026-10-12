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

  it('starts each active operation with the rows the review carries, in one request', async () => {
    const transport = targetServer(901)
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()

    expect(transport.request).toHaveBeenCalledTimes(1)
    expect(controller.targets('active-1')).toHaveLength(20)
    expect(controller.targets('active-1')[19]?.merchant).toBe('Example Merchant 20')
    expect(controller.targets('redo-1')).toEqual([])
  })

  it('adds one page of rows per request after the rows the review carries', async () => {
    const transport = targetServer(901)
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()

    await expect(controller.loadMore('active-1')).resolves.toBe(true)
    expect(controller.targets('active-1')).toHaveLength(420)
    await controller.loadMore('active-1')
    await controller.loadMore('active-1')

    const rows = controller.targets('active-1')
    expect(rows).toHaveLength(901)
    expect(rows[0]?.merchant).toBe('Example Merchant 1')
    expect(rows[900]?.merchant).toBe('Example Merchant 901')
    const windows = vi
      .mocked(transport.request)
      .mock.calls.slice(1)
      .map(([, body]) => (JSON.parse(body) as { window: unknown }).window)
    expect(windows).toEqual([
      { offset: 20, limit: 400 },
      { offset: 420, limit: 400 },
      { offset: 820, limit: 400 },
    ])
  })

  it('keeps each operation’s rows and reports which operation is loading', async () => {
    let release = (): void => undefined
    const transport = targetServer(30, () => new Promise<void>((resolve) => (release = resolve)))
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()

    const first = controller.loadMore('active-1')
    expect(controller.targetsLoading('active-1')).toBe(true)
    expect(controller.targetsLoading('active-2')).toBe(false)
    await vi.waitFor(() => expect(transport.request).toHaveBeenCalledTimes(2))
    release()
    await expect(first).resolves.toBe(true)
    expect(controller.targetsLoading('active-1')).toBe(false)
    const second = controller.loadMore('active-2')
    await vi.waitFor(() => expect(transport.request).toHaveBeenCalledTimes(3))
    release()
    await expect(second).resolves.toBe(true)

    expect(controller.targets('active-1').map((row) => row.merchant)).toEqual(
      merchants('Example', 30),
    )
    expect(controller.targets('active-2').map((row) => row.merchant)).toEqual(
      merchants('Other', 30),
    )
  })

  it('drops rows that arrive after the review reloads', async () => {
    let release = (): void => undefined
    const transport = targetServer(30, () => new Promise<void>((resolve) => (release = resolve)))
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()
    const stale = controller.loadMore('active-1')
    await vi.waitFor(() => expect(transport.request).toHaveBeenCalledTimes(2))
    const reload = controller.load()
    release()
    await expect(stale).resolves.toBe(false)
    await reload

    expect(controller.targets('active-1')).toHaveLength(20)
    expect(controller.targetsLoading('active-1')).toBe(false)
  })

  it('keeps the rows it loaded when a later request fails', async () => {
    const transport = targetServer(500)
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()
    vi.mocked(transport.request).mockRejectedValueOnce(new Error('offline'))

    await expect(controller.loadMore('active-1')).resolves.toBe(false)

    expect(controller.targets('active-1')).toHaveLength(20)
    expect(controller.targetsLoading('active-1')).toBe(false)
    expect(controller.state.announcement).toBe('Pending changes could not be loaded.')

    // Trying again clears the failure.
    await expect(controller.loadMore('active-1')).resolves.toBe(true)
    expect(controller.targets('active-1')).toHaveLength(420)
    expect(controller.state.announcement).toBe('')
  })

  it('keeps each operation’s targets when an older request answers last', async () => {
    const answers: Array<() => void> = []
    const transport = targetServer(30, () => new Promise<void>((resolve) => answers.push(resolve)))
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()

    const first = controller.loadMore('active-1')
    const second = controller.loadMore('active-2')
    await vi.waitFor(() => expect(answers).toHaveLength(2))
    answers[1]!()
    await expect(second).resolves.toBe(true)
    answers[0]!()
    await expect(first).resolves.toBe(true)

    expect(controller.targets('active-2').map((row) => row.merchant)).toEqual(
      merchants('Other', 30),
    )
    expect(controller.targets('active-1').map((row) => row.merchant)).toEqual(
      merchants('Example', 30),
    )
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

/** Merchant names `<prefix> Merchant 1` through `<prefix> Merchant <count>`. */
function merchants(prefix: string, count: number): string[] {
  return Array.from({ length: count }, (_, index) => `${prefix} Merchant ${index + 1}`)
}

/**
 * A review server with two active operations that each affect `affected` rows of their own
 * merchant, numbered by row. The review carries each active operation's first 20 rows, as the
 * server does, and a target request for an operation the review did not list fails.
 */
function targetServer(affected: number, wait?: () => Promise<void>): MutationFetch {
  const prefixes = new Map([
    ['active-1', 'Example'],
    ['active-2', 'Other'],
  ])
  const rows = (prefix: string, offset: number, limit: number) =>
    merchants(prefix, Math.min(offset + limit, affected))
      .slice(offset)
      .map((merchant) => ({ ...reviewResponse('7').targets[0]!, merchant }))
  const review = reviewResponse('7')
  const operation = review.active_operations[0]!
  const active = [...prefixes].map(([operation_id, prefix]) => ({
    ...operation,
    operation_id,
    affected_count: affected,
    targets: rows(prefix, 0, 20),
  }))
  const pending = {
    active_operations: 2,
    inactive_operations: 1,
    affected_transactions: 2 * affected,
  }
  return {
    request: vi.fn(async (path: string, body: string) => {
      if (!path.endsWith('/targets')) {
        return jsonResponse({ ...review, pending, active_operations: active })
      }
      const request = JSON.parse(body) as {
        operation_id: string
        window: { offset: number; limit: number }
      }
      const prefix = prefixes.get(request.operation_id)
      if (!prefix) throw new Error(`unknown operation ${request.operation_id}`)
      await wait?.()
      const { offset, limit } = request.window
      const targets = rows(prefix, offset, limit)
      const window = { offset, limit, count: targets.length }
      return jsonResponse({
        ...review,
        pending,
        active_operations: active,
        operation_id: request.operation_id,
        window,
        targets,
      })
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
