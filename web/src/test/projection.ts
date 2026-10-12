import type { ViewProjection } from '../lib/api/client'
import type { components } from '../lib/api/schema'

export function testProjection(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return {
    api_schema_version: '1',
    projection_schema_version: '1',
    revision: '0',
    profile_kind: 'local',
    amazon_match_column: false,
    pending: { active_operations: 0, inactive_operations: 0, affected_transactions: 0 },
    canonical_query: 'v=1',
    selection: 'mfsel1.example' as ViewProjection['selection'],
    selection_count: 0,
    all_rows_selected: false,
    view: {
      mode: 'detail',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
      sort_fields: ['merchant', 'count', 'amount'],
      drill_depth: 0,
      root: { mode: 'detail', grouping: 'merchant', time_granularity: 'year' },
    },
    breadcrumbs: [{ dimension: 'merchant', label: 'All transactions' }],
    breadcrumb_text: 'All transactions',
    filters: { show_hidden: false, show_transfers: false },
    capabilities: [],
    currencies: [],
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
    ...overrides,
  }
}

type Money = components['schemas']['Money']
type AggregateRow = components['schemas']['AggregateRow']

/** Builds exact money from minor units the way the server formats `display`. */
export function testMoney(minor: string, display: string): Money {
  const negative = minor.startsWith('-')
  const digits = (negative ? minor.slice(1) : minor).padStart(3, '0')
  const decimal = `${negative ? '-' : ''}${digits.slice(0, -2)}.${digits.slice(-2)}`
  return { minor, currency: 'USD', scale: 2, decimal, display }
}

export function testAggregateRow(overrides: Partial<AggregateRow> = {}): AggregateRow {
  return {
    index: 0,
    identity: 'agg-0',
    dimension: 'merchant',
    label: 'Example Merchant',
    count: 2,
    in: testMoney('300', '+3.00'),
    out: testMoney('-500', '-5.00'),
    total: testMoney('-200', '-2.00'),
    share_tenths: 62,
    flags: { selected: false, hidden: false, pending: false },
    ...overrides,
  }
}
