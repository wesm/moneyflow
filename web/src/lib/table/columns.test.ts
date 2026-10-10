import { describe, expect, it } from 'vitest'

import {
  columnsAtWidth,
  columnsFor,
  columnsThatFit,
  compactColumnCount,
  compactColumnIndex,
  compactLayout,
} from './columns'
import type { ViewProjection } from '../api/client'
import { testProjection } from '../../test/projection'

type View = ViewProjection['view']

function grouped(view: Partial<View>, overrides: Partial<ViewProjection> = {}) {
  return testProjection({
    view: {
      mode: 'aggregate',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
      sort_fields: ['merchant', 'count', 'amount'],
      ...view,
    },
    detail_rows: null,
    aggregate_rows: [],
    ...overrides,
  })
}

function detail(overrides: Partial<ViewProjection> = {}) {
  return testProjection({
    view: {
      mode: 'detail',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'date',
      sort_direction: 'desc',
      sort_fields: ['date', 'merchant', 'category', 'account', 'amount'],
    },
    ...overrides,
  })
}

const filters = testProjection().filters
/** A Sep 2026 scope, which has an August comparison. */
const dated = { filters: { ...filters, date_range: { from: '2026-09-01', to: '2026-09-30' } } }
const keys = (projection: ViewProjection) => columnsFor(projection).map((column) => column.key)
const labels = (projection: ViewProjection) => columnsFor(projection).map((column) => column.label)

describe('columnsFor', () => {
  it('lists the grouped columns with Context only for merchants and categories', () => {
    expect(keys(grouped({ grouping: 'merchant' }))).toEqual([
      'select',
      'name',
      'context',
      'count',
      'in',
      'out',
      'net',
      'share',
      'trend',
    ])
    expect(keys(grouped({ grouping: 'category' }))).toContain('context')
    expect(keys(grouped({ grouping: 'group' }))).toEqual([
      'select',
      'name',
      'count',
      'in',
      'out',
      'net',
      'share',
      'trend',
    ])
    expect(keys(grouped({ grouping: 'account' }))).not.toContain('context')
  })

  it('ends grouped views with Trend, then vs prior when the scope has a comparison', () => {
    const september = grouped({}, dated)
    expect(keys(september).slice(-3)).toEqual(['share', 'trend', 'vs_prior'])
    // All has no prior range to compare with.
    expect(keys(grouped({}))).toContain('trend')
    expect(keys(grouped({}))).not.toContain('vs_prior')
    const added = columnsFor(september, 'vs Aug').slice(-2)
    expect(added.map((column) => column.label)).toEqual(['Trend', 'vs Aug'])
    expect(added.every((column) => column.minWidth === 760 && !column.sortField)).toBe(true)
    // Until the insights name the comparison.
    expect(labels(september).at(-1)).toBe('vs prior')
  })

  it('widens vs prior to fit a long comparison label, and counts it in the minimum', () => {
    const september = grouped({}, dated)
    const width = (label: string) => columnsFor(september, label).at(-1)!
    expect(width('vs Aug')).toMatchObject({ width: '5.5rem', min: 5.5 })
    const long = ['vs 2025 to date', 'vs Feb 7 – Mar 9', 'vs Dec 7, 2025 – Jan 6, 2026']
    // 0.40 rem per character of the header font, plus the cell's padding, in quarter rems.
    expect(long.map((label) => width(label).min)).toEqual([7, 7.5, 12.25])
    for (const label of long) expect(width(label).width).toBe(`${width(label).min}rem`)
  })

  it('leaves Trend and vs prior out of transaction and Time views', () => {
    const time = grouped(
      { grouping: 'time', sort_fields: ['time_period', 'count', 'amount'] },
      dated,
    )
    for (const projection of [detail(dated), time]) {
      expect(keys(projection)).not.toContain('trend')
      expect(keys(projection)).not.toContain('vs_prior')
    }
  })

  it('names the current dimension in the Name header, including a sub-grouping', () => {
    expect(labels(grouped({ grouping: 'merchant' }))[1]).toBe('Merchant')
    // A drill keeps the detail mode while it breaks its transactions down by a sub-grouping.
    const subGrouped = grouped({ mode: 'detail', grouping: 'merchant', sub_grouping: 'category' })
    expect(labels(subGrouped)[1]).toBe('Category')
    expect(keys(subGrouped)).toContain('count')
  })

  it('labels Context by what it holds and drops it below 760 px', () => {
    const merchant = columnsFor(grouped({ grouping: 'merchant' }))
    const context = merchant.find((column) => column.key === 'context')
    expect(context?.label).toBe('Top category')
    expect(context?.minWidth).toBe(760)
    const category = columnsFor(grouped({ grouping: 'category' }))
    expect(category.find((column) => column.key === 'context')?.label).toBe('Group')
  })

  it('gives Count, In, Out, and Net one end-aligned width', () => {
    const metrics = columnsFor(grouped({})).filter((column) =>
      ['count', 'in', 'out', 'net'].includes(column.key),
    )
    expect(new Set(metrics.map((column) => column.width)).size).toBe(1)
    expect(metrics.every((column) => column.align === 'end')).toBe(true)
  })

  it('makes only the server sort fields sortable', () => {
    const sortable = columnsFor(grouped({ sort_fields: ['merchant', 'count', 'amount'] }))
      .filter((column) => column.sortField)
      .map((column) => [column.key, column.sortField])
    expect(sortable).toEqual([
      ['name', 'merchant'],
      ['count', 'count'],
      ['net', 'amount'],
    ])
    const narrower = columnsFor(grouped({ sort_fields: ['count'] }))
    expect(narrower.filter((column) => column.sortField).map((column) => column.key)).toEqual([
      'count',
    ])
  })

  it('lists the Time columns, also for a Time sub-grouping inside a drill', () => {
    const time = ['select', 'period', 'count', 'in', 'out', 'net', 'net_bar', 'savings']
    const top = grouped({ grouping: 'time', sort_fields: ['time_period', 'count', 'amount'] })
    expect(keys(top)).toEqual(time)
    expect(columnsFor(top).find((column) => column.key === 'period')?.sortField).toBe('time_period')
    const drilled = grouped({ mode: 'detail', grouping: 'merchant', sub_grouping: 'time' })
    expect(keys(drilled)).toEqual(time)
  })

  it('lists the transaction columns in spec order', () => {
    expect(keys(detail())).toEqual([
      'select',
      'date',
      'merchant',
      'category',
      'account',
      'amount',
      'flags',
    ])
    expect(labels(detail()).slice(1, 6)).toEqual([
      'Date',
      'Merchant',
      'Category',
      'Account',
      'Amount',
    ])
  })

  it('relabels Merchant as Product and Account as Order in Amazon profiles', () => {
    const amazon = labels(detail({ profile_kind: 'amazon' }))
    expect(amazon.slice(1, 6)).toEqual(['Date', 'Product', 'Category', 'Order', 'Amount'])
  })

  it('narrows the first drilled column by merchant, category, account precedence', () => {
    const drilled = (frames: { dimension: string; label: string }[]) =>
      detail({
        canonical_query: `v=1${frames.map((frame) => `&drill=${frame.dimension}`).join('')}`,
        breadcrumbs: frames,
      })
    const widthOf = (projection: ViewProjection, key: string) =>
      columnsFor(projection).find((column) => column.key === key)?.width
    const wide = widthOf(detail(), 'merchant')

    const category = drilled([{ dimension: 'category', label: 'Groceries' }])
    expect(widthOf(category, 'category')).toBe('11ch')
    expect(widthOf(category, 'merchant')).toBe(wide)

    const both = drilled([
      { dimension: 'category', label: 'Groceries' },
      { dimension: 'merchant', label: 'A merchant name far longer than thirty characters' },
    ])
    expect(widthOf(both, 'merchant')).toBe('30ch')
    expect(widthOf(both, 'category')).not.toBe('11ch')
    expect(keys(both)).toEqual(keys(detail()))

    const short = drilled([{ dimension: 'account', label: 'Card' }])
    expect(widthOf(short, 'account')).toBe('9ch')
    const amazon = detail({
      profile_kind: 'amazon',
      canonical_query: 'v=1&drill=account',
      breadcrumbs: [{ dimension: 'account', label: 'A1' }],
    })
    expect(widthOf(amazon, 'account')).toBe('7ch')
  })
})

describe('columnsAtWidth', () => {
  const merchants = () => columnsFor(grouped({ grouping: 'merchant' }, dated))
  const at = (wide: boolean) => columnsAtWidth(merchants(), wide).map((column) => column.key)

  it('drops Context, Trend, and vs prior below 760 px and keeps every other column', () => {
    expect(keys(grouped({ grouping: 'merchant' }, dated))).toContain('vs_prior')
    expect(at(false)).toEqual(['select', 'name', 'count', 'in', 'out', 'net', 'share'])
  })

  it('keeps Context, Trend, and vs prior from 760 px', () => {
    expect(at(true)).toEqual(keys(grouped({ grouping: 'merchant' }, dated)))
  })
})

describe('columnsThatFit', () => {
  const merchants = columnsFor(grouped({ grouping: 'merchant' }, dated))
  const fit = (rem: number | undefined) =>
    columnsThatFit(merchants, rem).map((column) => column.key)
  const all = merchants.map((column) => column.key)
  // ☐ 2, Name 10, Context 7, Count, In, Out, Net, and Share 6.5 each, Trend 4.5, vs prior 5.5.
  const everything = 61.5

  it('keeps every column when they fit with 0.25 rem to spare, or when there is no measure', () => {
    expect(fit(undefined)).toEqual(all)
    expect(fit(everything + 0.25)).toEqual(all)
    expect(fit(100)).toEqual(all)
  })

  it('drops a column at an exact fit, where sub-pixel rounding could scroll the table', () => {
    expect(fit(everything)).toEqual(all.filter((key) => key !== 'context'))
    expect(fit(everything + 0.2)).toEqual(all.filter((key) => key !== 'context'))
  })

  it('drops Context first, then Trend and vs prior together', () => {
    expect(fit(54.75)).toEqual(all.filter((key) => key !== 'context'))
    expect(fit(54.5)).toEqual(['select', 'name', 'count', 'in', 'out', 'net', 'share'])
  })

  it('never drops the columns that carry the row’s totals', () => {
    expect(fit(10)).toEqual(['select', 'name', 'count', 'in', 'out', 'net', 'share'])
  })

  it('leaves transaction and Time views alone', () => {
    const transactions = columnsFor(detail(dated))
    expect(columnsThatFit(transactions, 10)).toEqual(transactions)
    const time = columnsFor(
      grouped({ grouping: 'time', sort_fields: ['time_period', 'count', 'amount'] }, dated),
    )
    expect(columnsThatFit(time, 10)).toEqual(time)
  })
})

describe('compactColumnIndex', () => {
  it('numbers each phone layout from the selection box to the trailing button', () => {
    expect(compactColumnIndex('detail', 'select')).toBe(1)
    expect(compactColumnIndex('detail', 'amount')).toBe(3)
    expect(compactColumnIndex('detail', 'actions')).toBe(compactColumnCount('detail'))
    expect(compactColumnIndex('aggregate', 'net')).toBe(3)
    expect(compactColumnIndex('time', 'period')).toBe(2)
  })

  it('picks the layout from the rows the projection holds', () => {
    expect(compactLayout(detail())).toBe('detail')
    expect(compactLayout(grouped({ grouping: 'category' }))).toBe('aggregate')
    expect(compactLayout(grouped({ grouping: 'time' }))).toBe('time')
  })
})
