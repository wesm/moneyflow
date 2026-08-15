import type { ViewProjection } from '../lib/api/client'

export function testProjection(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return {
    api_schema_version: '1',
    projection_schema_version: '1',
    revision: '0',
    pending: { active_operations: 0, inactive_operations: 0, affected_transactions: 0 },
    canonical_query: 'v=1',
    selection: 'mfsel1.example' as ViewProjection['selection'],
    selection_count: 0,
    view: {
      mode: 'detail',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
    },
    breadcrumbs: [{ dimension: 'merchant', label: 'All transactions' }],
    breadcrumb_text: 'All transactions',
    filters: { show_hidden: false, show_transfers: false },
    capabilities: [],
    total_rows: 1,
    window: { offset: 0, limit: 200, count: 1 },
    detail_rows: [
      {
        index: 0,
        identity: 'row-0',
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
        flags: { selected: true, hidden: false, pending: false },
      },
    ],
    statistics: [],
    chart: {},
    ...overrides,
  }
}
