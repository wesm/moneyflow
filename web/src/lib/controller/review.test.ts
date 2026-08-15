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

  it('fetches target details only on demand and retains the current and adjacent windows', async () => {
    const transport: MutationFetch = {
      request: vi.fn(async (path, body) => {
        const request = JSON.parse(body) as { window?: { offset: number; limit: number } }
        return jsonResponse(
          path.endsWith('/targets')
            ? reviewResponse('7', request.window?.offset ?? 0, request.window?.limit ?? 100)
            : reviewResponse('7'),
        )
      }),
    }
    const controller = createReviewController({ transport, revision: () => 7n })
    await controller.load()
    expect(transport.request).toHaveBeenCalledTimes(1)

    await controller.loadTargets('active-1', 0)
    await controller.loadTargets('active-1', 100)
    await controller.loadTargets('active-1', 200)
    await controller.loadTargets('active-1', 300)

    expect(controller.targetOffsets('active-1')).toEqual([200, 300])
    expect(transport.request).toHaveBeenCalledTimes(5)
    const [, rawBody] = vi.mocked(transport.request).mock.calls[4]!
    expect(JSON.parse(rawBody)).toMatchObject({
      expected_revision: '7',
      operation_id: 'active-1',
      window: { offset: 300, limit: 100 },
    })
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
      { date: '2024-01-01', merchant: 'Example Merchant', category: 'Category', hidden: false },
    ],
  }
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}
