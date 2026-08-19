import { describe, expect, it, vi } from 'vitest'

import type {
  DuplicateResponse,
  MoneyflowClient,
  MutationFetch,
  SelectionValue,
} from '../api/client'
import { testProjection } from '../../test/projection'
import { createDuplicateController } from './duplicates'

describe('DuplicateController', () => {
  it('keeps selection transient and toggles against a derived detail query', async () => {
    const current = testProjection({
      revision: '3',
      canonical_query: 'group=category&subgroup=merchant&v=1',
    })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates)
      .mockResolvedValueOnce(duplicates())
      .mockResolvedValueOnce(duplicates({ selection: selection('selected'), selection_count: 1 }))
    vi.mocked(client.transition).mockResolvedValue(
      testProjection({ selection: selection('selected'), selection_count: 1 }),
    )
    const controller = createDuplicateController({
      client,
      mutations: mutationTransport(),
      host: { current: () => current, recheck: vi.fn(async () => current) },
    })

    await expect(controller.open()).resolves.toBe(true)
    expect(client.projectDuplicates).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({ query: current.canonical_query, expected_revision: '3' }),
      undefined,
    )
    await expect(controller.toggleFocused()).resolves.toBe(true)
    expect(client.transition).toHaveBeenCalledWith(
      expect.objectContaining({
        query: 'group=category&mode=detail&v=1',
        action: 'selection.toggle',
        target: { kind: 'transaction', identity: 'txn-1' },
      }),
    )
    expect(controller.state.projection?.selection_count).toBe(1)
    expect(current.selection_count).toBe(0)
    expect(location.search).toBe('')
  })

  it('stages deletion only after confirmation and reloads remote projections', async () => {
    let current = testProjection({ revision: '3' })
    const next = testProjection({ revision: '4' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates)
      .mockResolvedValueOnce(duplicates({ selection: selection('selected'), selection_count: 1 }))
      .mockResolvedValueOnce(
        duplicates({ revision: '4', total_groups: 0, total_transactions: 0, groups: [] }),
      )
    const transport = mutationTransport(
      Response.json({
        version: '1',
        revision: '4',
        canonical_query: 'mode=detail&v=1',
        projection: next,
        pending: { active_operations: 1, inactive_operations: 0, affected_transactions: 1 },
        selection: { kind: 'cleared', value: selection('empty') },
      }),
    )
    const recheck = vi.fn(async () => {
      current = next
      return next
    })
    const controller = createDuplicateController({
      client,
      mutations: transport,
      host: { current: () => current, recheck },
    })

    await controller.open()
    controller.requestDelete()
    expect(controller.state.phase).toBe('confirming')
    expect(vi.mocked(transport.request)).not.toHaveBeenCalled()
    await expect(controller.confirmDelete()).resolves.toBe(true)
    expect(transport.request).toHaveBeenCalledTimes(1)
    const body = JSON.parse(vi.mocked(transport.request).mock.calls[0]?.[1] ?? '{}') as {
      action?: string
      selection?: string
      query?: string
    }
    expect(body).toMatchObject({
      action: 'transaction.delete',
      selection: selection('selected'),
      query: 'mode=detail&v=1',
    })
    expect(recheck).toHaveBeenCalledWith(current.selection, true)
    expect(controller.state.projection?.total_groups).toBe(0)
    expect(controller.state.announcement).toContain('Staged deletion')
  })

  it('never replays a stale deletion automatically', async () => {
    const current = testProjection({ revision: '3' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates).mockResolvedValue(duplicates())
    const transport = mutationTransport(
      Response.json(
        {
          type: 'about:blank',
          title: 'Conflict',
          status: 409,
          detail: 'The profile changed.',
          code: 'revision_conflict',
          current_revision: '4',
        },
        { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
      ),
    )
    const recheck = vi.fn(async () => testProjection({ revision: '4' }))
    const controller = createDuplicateController({
      client,
      mutations: transport,
      host: { current: () => current, recheck },
    })

    await controller.open()
    controller.requestDelete()
    await expect(controller.confirmDelete()).resolves.toBe(false)
    expect(transport.request).toHaveBeenCalledTimes(1)
    expect(recheck).toHaveBeenCalled()
    expect(controller.state.phase).toBe('conflict')
    expect(controller.state.announcement).toContain('Invoke deletion again')
  })

  it('advances the group window after exhausting rows in the current groups', async () => {
    const current = testProjection({ revision: '3' })
    const client = duplicateClient()
    vi.mocked(client.projectDuplicates)
      .mockResolvedValueOnce(
        duplicates({
          total_groups: 201,
          total_transactions: 402,
          group_window: { offset: 0, limit: 200, count: 200 },
          row_window: { offset: 0, limit: 200, count: 2 },
        }),
      )
      .mockResolvedValueOnce(
        duplicates({
          group_window: { offset: 200, limit: 200, count: 1 },
          row_window: { offset: 0, limit: 200, count: 2 },
        }),
      )
    const controller = createDuplicateController({
      client,
      mutations: mutationTransport(),
      host: { current: () => current, recheck: vi.fn(async () => current) },
    })

    await controller.open()
    await expect(controller.page(1)).resolves.toBe(true)
    expect(client.projectDuplicates).toHaveBeenLastCalledWith(
      expect.objectContaining({
        group_window: { offset: 200, limit: 200 },
        row_window: { offset: 0, limit: 200 },
      }),
      undefined,
    )
  })
})

function duplicateClient(): MoneyflowClient {
  return {
    mutations: mutationTransport(),
    view: vi.fn(),
    transition: vi.fn(),
    projectDuplicates: vi.fn(),
    providerStatus: vi.fn(),
    providerWriteStatus: vi.fn(),
  }
}

function mutationTransport(response = Response.json({})): MutationFetch {
  return { request: vi.fn(async () => response.clone()) }
}

function selection(value: string): SelectionValue {
  return `mfsel1.${value}` as SelectionValue
}

function duplicates(overrides: Partial<DuplicateResponse> = {}): DuplicateResponse {
  return {
    version: '1',
    revision: '3',
    canonical_query: 'v=1',
    selection: selection('empty'),
    selection_count: 0,
    total_groups: 1,
    total_transactions: 2,
    group_window: { offset: 0, limit: 200, count: 1 },
    row_window: { offset: 0, limit: 200, count: 2 },
    groups: [
      {
        number: 1,
        rows: [duplicateRow('txn-1'), duplicateRow('txn-2')],
      },
    ],
    ...overrides,
  }
}

function duplicateRow(identity: string) {
  return {
    group_number: 1,
    target: { kind: 'transaction' as const, identity },
    date: '2024-01-02',
    account: 'Account Name',
    merchant: 'Example Merchant',
    category: 'Example Category',
    group: 'Example Group',
    amount: {
      minor: '-1234',
      currency: 'USD',
      scale: 2,
      decimal: '-12.34',
      display: '-$12.34',
    },
    matching_label: 'Example Merchant',
    flags: { selected: false, hidden: false, pending: false },
  }
}
