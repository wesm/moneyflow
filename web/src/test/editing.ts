import { vi } from 'vitest'
import type { EditorCatalog } from '../lib/api/client'
import type { EditingController } from '../lib/controller/editing'
import type { ReviewController } from '../lib/controller/review'

export const testCatalog: EditorCatalog = {
  version: '1',
  revision: '1',
  merchants: [
    {
      id: 'merchant-a',
      label: 'Example Merchant',
      protected: false,
      transaction_count: 4,
      in_use: true,
    },
  ],
  categories: [
    {
      id: 'category-uncategorized',
      label: 'Uncategorized',
      protected: true,
      transaction_count: 0,
      in_use: false,
    },
    {
      id: 'category-a',
      label: 'Example Category',
      parent_id: 'group-a',
      protected: false,
      transaction_count: 4,
      in_use: true,
    },
  ],
  groups: [
    { id: 'group-a', label: 'Example Group', protected: false, transaction_count: 4, in_use: true },
  ],
}

export function testEditingController(
  overrides: Partial<EditingController> = {},
): EditingController {
  return {
    state: {
      revision: 1n,
      phase: 'idle',
      pending: { active_operations: 1, inactive_operations: 0, affected_transactions: 2 },
      announcement: '',
    },
    holdForKey: vi.fn(() => ({
      run: async (work: () => unknown) => {
        await work()
      },
    })),
    sync: vi.fn(),
    submit: vi.fn(async () => true),
    undo: vi.fn(async () => true),
    redo: vi.fn(async () => true),
    commit: vi.fn(async () => true),
    catalog: vi.fn(async () => testCatalog),
    stop: vi.fn(),
    previewMerchant: vi.fn(async () => ({
      version: '1',
      revision: '1',
      affected_transactions: 5,
      outside_view: 0,
      outside_currencies: [],
      transactions: [
        {
          index: 0,
          identity: 'transaction-a',
          date: '2024-01-01',
          account: 'Example Account',
          merchant: 'Current Merchant',
          category: 'Example Category',
          group: 'Example Group',
          amount: { minor: '-100', currency: 'USD', scale: 2, decimal: '-1.00', display: '-$1.00' },
          flags: { selected: false, hidden: false, pending: false },
        },
      ],
    })),
    ...overrides,
  }
}

export function testReviewController(overrides: Partial<ReviewController> = {}): ReviewController {
  return {
    state: {
      phase: 'idle',
      reviewedRevision: 1n,
      pending: { active_operations: 1, inactive_operations: 1, affected_transactions: 2 },
      activeOperations: [
        {
          operation_id: 'operation-a',
          type: 'merchant.label',
          label: 'Rename merchant',
          active: true,
          affected_count: 2,
        },
      ],
      inactiveOperations: [
        {
          operation_id: 'operation-b',
          type: 'hide.toggle',
          label: 'Hide transactions',
          active: false,
          affected_count: 1,
        },
      ],
      announcement: '',
    },
    load: vi.fn(async () => true),
    loadMore: vi.fn(async () => true),
    targets: vi.fn(() => [
      {
        date: '2024-01-01',
        merchant: 'Example Merchant',
        category: 'Example Category',
        amount: {
          minor: '-1234',
          currency: 'USD',
          scale: 2,
          decimal: '-12.34',
          display: '-12.34',
        },
        hidden: false,
      },
    ]),
    targetsLoading: vi.fn(() => false),
    isStale: vi.fn(() => false),
    clear: vi.fn(),
    ...overrides,
  }
}
