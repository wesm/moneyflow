import { describe, expect, it } from 'vitest'

import { merchantScope } from './merchant-scope'
import { testAggregateRow, testProjection } from '../../test/projection'

const grouped = (dimension: string) =>
  testProjection({
    detail_rows: null,
    aggregate_rows: [
      testAggregateRow({ index: 0, identity: 'agg-0', dimension }),
      testAggregateRow({ index: 1, identity: 'agg-1', dimension }),
    ],
    total_rows: 2,
  })

const merchantDrill = testProjection({
  view: { ...testProjection().view, drill_depth: 1 },
  breadcrumbs: [{ dimension: 'merchant', label: 'Example Merchant' }],
})

describe('merchantScope', () => {
  it('allows the whole merchant from a merchant row with nothing selected', () => {
    expect(merchantScope(grouped('merchant'), 1, 0)).toEqual({ entity: true })
  })

  it('allows the whole merchant from any row inside a merchant drill', () => {
    expect(merchantScope(merchantDrill, 0, 0)).toEqual({ entity: true })
  })

  it('asks to clear a selection first, even on a merchant row', () => {
    expect(merchantScope(grouped('merchant'), 0, 2)).toEqual({
      entity: false,
      reason: 'Clear the selection to rename the whole merchant',
    })
    expect(merchantScope(merchantDrill, 0, 1).entity).toBe(false)
  })

  it('asks to open a merchant from rows that group by something else', () => {
    const reason = 'Open a merchant to rename all its transactions'
    expect(merchantScope(grouped('category'), 0, 0)).toEqual({ entity: false, reason })
    expect(merchantScope(testProjection(), 0, 0)).toEqual({ entity: false, reason })
  })

  it('does not count the top-level breadcrumb as a merchant drill', () => {
    // At the top level the single breadcrumb names the grouping, not a drilled merchant.
    const top = testProjection({ breadcrumbs: [{ dimension: 'merchant', label: 'All' }] })
    expect(merchantScope(top, 0, 0).entity).toBe(false)
  })

  it('reads the row under the cursor, not the first row', () => {
    const mixed = testProjection({
      detail_rows: null,
      aggregate_rows: [
        testAggregateRow({ index: 0, dimension: 'category' }),
        testAggregateRow({ index: 1, identity: 'agg-1', dimension: 'merchant' }),
      ],
    })
    expect(merchantScope(mixed, 1, 0).entity).toBe(true)
    expect(merchantScope(mixed, 0, 0).entity).toBe(false)
    expect(merchantScope(mixed, 5, 0).entity).toBe(false)
  })
})
