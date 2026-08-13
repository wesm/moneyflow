import { describe, expect, it } from 'vitest'

import { WindowCache, preserveCursor } from './windows'
import type { ViewProjection } from '../api/client'

describe('bounded projection windows', () => {
  it('keeps only current and adjacent aligned windows for one analytical query', () => {
    const cache = new WindowCache(200)
    cache.store(projection('v=1', 0, 1000))
    cache.store(projection('v=1', 200, 1000))
    cache.store(projection('v=1', 400, 1000))
    cache.retainAdjacent('v=1', 200)

    expect(cache.offsets('v=1')).toEqual([0, 200, 400])
    cache.store(projection('v=1', 600, 1000))
    cache.retainAdjacent('v=1', 600)
    expect(cache.offsets('v=1')).toEqual([400, 600])
  })

  it('never merges rows across canonical queries', () => {
    const cache = new WindowCache(200)
    cache.store(projection('v=1', 0, 500))
    cache.store(projection('v=1&search=x', 0, 1))
    expect(cache.get('v=1', 0)?.total_rows).toBe(500)
    expect(cache.get('v=1&search=x', 0)?.total_rows).toBe(1)
  })

  it('preserves cursor identity, then clamps an absent absolute index', () => {
    const current = projection('v=1', 200, 250)
    expect(preserveCursor(current, 'row-203', 1)).toEqual({ identity: 'row-203', index: 203 })
    expect(preserveCursor(current, 'missing', 999)).toEqual({ identity: 'row-249', index: 249 })
    expect(preserveCursor(projection('v=1', 0, 0), undefined, 4)).toEqual({
      identity: undefined,
      index: 0,
    })
  })
})

function projection(query: string, offset: number, total: number): ViewProjection {
  const count = Math.max(0, Math.min(200, total - offset))
  return {
    api_schema_version: '1',
    projection_schema_version: '1',
    canonical_query: query,
    view: {
      mode: 'detail',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'date',
      sort_direction: 'desc',
    },
    selection: 'mfsel1.example' as ViewProjection['selection'],
    breadcrumbs: [],
    breadcrumb_text: 'All transactions',
    filters: { show_hidden: false, show_transfers: false },
    capabilities: [],
    total_rows: total,
    window: { offset, limit: 200, count },
    detail_rows: Array.from({ length: count }, (_, index) => ({
      index: offset + index,
      identity: `row-${offset + index}`,
      date: '2024-01-01',
      account: 'Account Name',
      merchant: 'Example Merchant',
      category: 'Example Category',
      group: 'Example Group',
      amount: { minor: '-100', currency: 'USD', scale: 2, decimal: '-1.00', display: '-$1.00' },
      flags: { selected: false, hidden: false, pending: false },
    })),
    statistics: [],
    chart: {},
  }
}
