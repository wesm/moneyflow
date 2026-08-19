import { vi } from 'vitest'
import type { EditorCatalog } from '../lib/api/client'
import type { EditingController } from '../lib/controller/editing'
import type { ReviewController } from '../lib/controller/review'

export const testCatalog: EditorCatalog = {
  version: '1',
  revision: '1',
  merchants: [{ id: 'merchant-a', label: 'Example Merchant', protected: false }],
  categories: [
    { id: 'category-uncategorized', label: 'Uncategorized', protected: true },
    { id: 'category-a', label: 'Example Category', parent_id: 'group-a', protected: false },
  ],
  groups: [{ id: 'group-a', label: 'Example Group', protected: false }],
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
    sync: vi.fn(),
    submit: vi.fn(async () => true),
    undo: vi.fn(async () => true),
    redo: vi.fn(async () => true),
    commit: vi.fn(async () => true),
    catalog: vi.fn(async () => testCatalog),
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
          label: 'Toggle report visibility',
          active: false,
          affected_count: 1,
        },
      ],
      announcement: '',
    },
    load: vi.fn(async () => true),
    loadTargets: vi.fn(async () => true),
    targets: vi.fn(() => [
      {
        date: '2024-01-01',
        merchant: 'Example Merchant',
        category: 'Example Category',
        hidden: false,
      },
    ]),
    targetOffsets: vi.fn(() => [0]),
    isStale: vi.fn(() => false),
    clear: vi.fn(),
    ...overrides,
  }
}
