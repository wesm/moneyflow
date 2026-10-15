import { describe, expect, it } from 'vitest'

import type { ViewProjection } from '../api/client'
import type { components } from '../api/schema'
import { ColumnSizer } from './column-sizer'
import { columnsFor } from './columns'
import { testAggregateRow, testProjection } from '../../test/projection'

type DetailRow = components['schemas']['DetailRow']

const base = testProjection().detail_rows![0]!

function row(identity: string, fields: Partial<DetailRow> = {}): DetailRow {
  return { ...base, identity, merchant: 'Shop', category: 'Food', account: 'Card', ...fields }
}

function window(rows: DetailRow[], offset = 0): ViewProjection {
  return testProjection({
    detail_rows: rows,
    window: { offset, limit: rows.length, count: rows.length },
  })
}

const detailColumns = columnsFor(testProjection())
const merchantColumns = detailColumns.filter((column) => column.key === 'merchant')
const ids = (rows: { identity: string }[]) => rows.map((kept) => kept.identity).sort()

describe('ColumnSizer', () => {
  it('keeps the rows with the longest different labels of each text column', () => {
    const repeated = Array.from({ length: 8 }, (_, index) =>
      row(`long-${index}`, { account: 'Example Savings Account' }),
    )
    const rows = [
      ...repeated,
      row('checking', { account: 'Everyday Checking' }),
      row('merchant', { merchant: 'Riverside Farmers Market' }),
      row('category', { category: 'Credit Card Payment' }),
    ]
    const sized = new ColumnSizer().rows('v=1 1', [window(rows)], detailColumns, 'merchant')
    // One row stands for the repeated label, so it cannot crowd out a shorter one.
    expect(sized.filter((kept) => kept.identity.startsWith('long-'))).toHaveLength(1)
    expect(ids(sized)).toEqual(expect.arrayContaining(['checking', 'merchant', 'category']))
  })

  it('keeps five labels per column, longest first', () => {
    const rows = ['a', 'bb', 'ccc', 'dddd', 'eeeee', 'ffffff'].map((merchant) =>
      row(merchant, { merchant }),
    )
    const sized = new ColumnSizer().rows('v=1 1', [window(rows)], merchantColumns, 'merchant')
    expect(ids(sized)).toEqual(['bb', 'ccc', 'dddd', 'eeeee', 'ffffff'])
  })

  it('ranks labels by how wide they draw, not how many characters they hold', () => {
    const narrow = ['ill 1', 'ill 22', 'ill 333', 'ill 4444', 'ill 55555']
    const rows = ['WWW', ...narrow].map((merchant) => row(merchant, { merchant }))
    const drawn = (text: string) => [...text].reduce((sum, c) => sum + (c === 'W' ? 3 : 1), 0)
    const sized = new ColumnSizer(drawn).rows('v=1 1', [window(rows)], merchantColumns, 'merchant')
    expect(ids(sized)).toEqual(['WWW', 'ill 22', 'ill 333', 'ill 4444', 'ill 55555'])
  })

  it('counts a staged edit as the old and new names the cell shows', () => {
    const staged = row('staged', {
      merchant: 'Shop',
      staged: { deleted: false, merchant: 'Store' },
    })
    const plain = row('plain', { merchant: 'Example Shop' })
    // Five shorter labels than "Shop → Store" still outrank "Shop" alone.
    const others = ['Cafe A', 'Cafe BB', 'Cafe CCC', 'Cafe DDDD', 'Cafe EEEEE'].map((merchant) =>
      row(merchant, { merchant }),
    )
    const sized = new ColumnSizer().rows(
      'v=1 1',
      [window([plain, staged, ...others])],
      merchantColumns,
      'merchant',
    )
    // "Shop → Store" is 12 characters, as long as "Example Shop".
    expect(ids(sized)).toContain('staged')
  })

  it('only widens while the view stays, and starts over for a new view or revision', () => {
    const sizer = new ColumnSizer()
    const long = row('long', { merchant: 'Riverside Farmers Market' })
    const first = window([long, row('short', { merchant: 'Cafe' })])
    expect(ids(sizer.rows('v=1 1', [first], merchantColumns, 'merchant'))).toContain('long')

    // A later window without the long label keeps it.
    const later = window([row('later', { merchant: 'Taco Truck' })], 200)
    expect(ids(sizer.rows('v=1 1', [later], merchantColumns, 'merchant'))).toEqual([
      'later',
      'long',
      'short',
    ])
    expect(ids(sizer.rows('v=1 2', [later], merchantColumns, 'merchant'))).toEqual(['later'])
    expect(ids(sizer.rows('sort=date v=1 2', [first], merchantColumns, 'merchant'))).toEqual([
      'long',
      'short',
    ])
  })

  // Character counts only approximate drawn width: "WIDE MART" can draw wider than the labels
  // with more characters that arrive later, so dropping it could narrow the column mid-scroll.
  it('keeps a label that sized a column when a later window brings five longer ones', () => {
    const sizer = new ColumnSizer()
    const wide = row('wide', { merchant: 'WIDE MART' })
    expect(ids(sizer.rows('v=1 1', [window([wide])], merchantColumns, 'merchant'))).toEqual([
      'wide',
    ])
    const longer = ['ice cafe 1', 'ice cafe 22', 'ice cafe 333', 'ice cafe 4444', 'ice cafe 55555']
    const later = window(
      longer.map((merchant) => row(merchant, { merchant })),
      200,
    )
    expect(ids(sizer.rows('v=1 1', [later], merchantColumns, 'merchant'))).toEqual(
      ['wide', ...longer].sort(),
    )
  })

  // A selection toggle answers with new row objects that hold the same labels.
  it('returns the same rows while a new window holds the same labels', () => {
    const sizer = new ColumnSizer()
    const labels = ['Cafe', 'Taco Truck', 'Example Shop']
    const make = () => window(labels.map((merchant) => row(merchant, { merchant })))
    const first = sizer.rows('v=1 1', [make()], merchantColumns, 'merchant')
    expect(sizer.rows('v=1 1', [make()], merchantColumns, 'merchant')).toBe(first)
  })

  it('sizes a grouped view by its row names and Context labels', () => {
    const names = ['Example Merchant A', 'Example Merchant B', 'Example Merchant C']
    const long = [...names, 'Example Merchant D', 'Example Merchant E'].map((label) =>
      testAggregateRow({ identity: label, label }),
    )
    const grouped = testProjection({
      detail_rows: null,
      aggregate_rows: [
        ...long,
        testAggregateRow({ identity: 'context', label: 'Cafe', top_category: 'Groceries' }),
      ],
      view: { ...testProjection().view, mode: 'aggregate', grouping: 'merchant' },
    })
    const sized = new ColumnSizer().rows('v=1 1', [grouped], columnsFor(grouped), 'merchant')
    // The short name holds the longest Context label, "Groceries 0%".
    expect(ids(sized)).toContain('context')
  })
})
