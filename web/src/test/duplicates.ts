import type { DuplicateResponse, SelectionValue } from '../lib/api/client'

export function testDuplicates(overrides: Partial<DuplicateResponse> = {}): DuplicateResponse {
  return {
    version: '1',
    revision: '3',
    canonical_query: 'v=1',
    selection: 'mfsel1.empty' as SelectionValue,
    selection_count: 0,
    total_groups: 1,
    total_transactions: 2,
    window_transactions: 2,
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
