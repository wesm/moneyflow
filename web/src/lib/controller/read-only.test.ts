import { describe, expect, it, vi } from 'vitest'

import { readOnlyClient, restrictCapabilities } from './read-only'
import { MoneyflowProblem, type MoneyflowClient } from '../api/client'
import { testCapabilities } from '../../test/capabilities'
import { testProjection } from '../../test/projection'

const ids = [
  'cursor.down',
  'view.set-grouping',
  'view.drill',
  'transaction.show-info',
  'transaction.edit-merchant',
  'transaction.edit-category',
  'transaction.toggle-hidden',
  'transaction.delete',
  'category.manage',
  'category-group.manage',
  'changes.undo',
  'changes.redo',
  'changes.review',
  'provider.refresh',
  'provider.refresh-history',
  'transactions.export',
  'view.find-duplicates',
]

describe('read-only mode', () => {
  it('leaves navigation available and makes every change unavailable with a reason', () => {
    const restricted = restrictCapabilities(testProjection({ capabilities: testCapabilities(ids) }))

    const unavailable = (restricted.capabilities ?? []).filter(
      (capability) => !capability.available,
    )
    expect(unavailable.map((capability) => capability.id)).toEqual(ids.slice(4))
    expect(new Set(unavailable.map((capability) => capability.reason))).toEqual(
      new Set(['Read-only mode']),
    )
  })

  it('restricts its projections and sends no provider status request', async () => {
    const projection = testProjection({ capabilities: testCapabilities(ids) })
    const providerStatus = vi.fn()
    const client = readOnlyClient({
      view: vi.fn(async () => projection),
      transition: vi.fn(async () => projection),
      providerStatus,
    } as unknown as MoneyflowClient)

    for (const loaded of [await client.view({} as never), await client.transition({} as never)]) {
      expect(loaded.capabilities?.find(({ id }) => id === 'changes.undo')?.available).toBe(false)
      expect(loaded.capabilities?.find(({ id }) => id === 'view.drill')?.available).toBe(true)
    }
    const status = client.providerStatus()
    await expect(status).rejects.toBeInstanceOf(MoneyflowProblem)
    await expect(status).rejects.toThrow('Read-only mode')
    expect(providerStatus).not.toHaveBeenCalled()
  })

  it('refuses every change request and sends none to the host', async () => {
    const request = vi.fn(async () => Response.json({}))
    const upload = vi.fn(async () => Response.json({}))
    const { mutations } = readOnlyClient({ mutations: { request, upload } } as never)

    for (const sent of [
      mutations.request('api/v1/mutations', '{}'),
      mutations.upload?.('api/v1/amazon/imports/example/files', new FormData()),
    ]) {
      await expect(sent).rejects.toMatchObject({ problem: { code: 'read_only' } })
    }
    expect(request).not.toHaveBeenCalled()
    expect(upload).not.toHaveBeenCalled()
  })
})
