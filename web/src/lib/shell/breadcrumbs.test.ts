import { describe, expect, it } from 'vitest'

import { breakDownOptions, breadcrumbTrail } from './breadcrumbs'
import { testProjection } from '../../test/projection'
import type { ViewProjection } from '../api/client'

const base = testProjection()

function view(overrides: Partial<ViewProjection['view']>): ViewProjection['view'] {
  return { ...base.view, ...overrides }
}

function root(
  mode: string,
  grouping: string,
  time_granularity = 'year',
): ViewProjection['view']['root'] {
  return { mode, grouping, time_granularity }
}

const topLevel = testProjection({
  view: view({ mode: 'aggregate', grouping: 'merchant', root: root('aggregate', 'merchant') }),
  breadcrumbs: [{ dimension: 'merchant', label: 'Merchants' }],
})
const depthOne = testProjection({
  view: view({
    mode: 'detail',
    grouping: 'merchant',
    drill_depth: 1,
    root: root('aggregate', 'merchant'),
  }),
  breadcrumbs: [{ dimension: 'merchant', label: 'Corner Grocery' }],
})
const depthTwo = testProjection({
  view: view({
    mode: 'detail',
    grouping: 'merchant',
    drill_depth: 2,
    root: root('aggregate', 'merchant'),
  }),
  breadcrumbs: [
    { dimension: 'merchant', label: 'Corner Grocery' },
    { dimension: 'category', label: 'Groceries' },
  ],
})

describe('breadcrumbTrail', () => {
  it('names the grouping at the top level, as the current page', () => {
    expect(breadcrumbTrail(topLevel)).toEqual([{ label: 'Merchants', current: true }])
    const categories = testProjection({
      view: view({ mode: 'aggregate', grouping: 'category', root: root('aggregate', 'category') }),
      breadcrumbs: [{ dimension: 'category', label: 'Categories' }],
    })
    expect(breadcrumbTrail(categories)).toEqual([{ label: 'Categories', current: true }])
  })

  it('names the d view All transactions', () => {
    const detail = testProjection({
      view: view({ mode: 'detail', grouping: 'account', root: root('detail', 'account') }),
      breadcrumbs: [{ dimension: 'account', label: 'Transactions' }],
    })
    expect(breadcrumbTrail(detail)).toEqual([{ label: 'All transactions', current: true }])
  })

  it('adds one crumb per drill frame, each returning to its level', () => {
    expect(breadcrumbTrail(depthOne)).toEqual([
      { label: 'Merchants', depth: 0, current: false },
      { label: 'Corner Grocery', current: true },
    ])
    expect(breadcrumbTrail(depthTwo)).toEqual([
      { label: 'Merchants', depth: 0, current: false },
      { label: 'Corner Grocery', depth: 1, current: false },
      { label: 'Groceries', current: true },
    ])
  })

  it('ends a sub-grouped drill with the breakdown; the frame returns to its transactions', () => {
    const subGrouped = { ...depthOne, view: { ...depthOne.view, sub_grouping: 'category' } }
    expect(breadcrumbTrail(subGrouped)).toEqual([
      { label: 'Merchants', depth: 0, current: false },
      { label: 'Corner Grocery', depth: 1, current: false },
      { label: 'by Category', current: true },
    ])
  })

  it('names a Time breakdown by its unit', () => {
    for (const [unit, label] of [
      ['day', 'by Day'],
      ['month', 'by Month'],
      ['year', 'by Year'],
    ]) {
      const subGrouped = {
        ...depthOne,
        view: { ...depthOne.view, sub_grouping: 'time', time_granularity: unit! },
      }
      expect(breadcrumbTrail(subGrouped).at(-1)).toEqual({ label, current: true })
    }
  })

  it('names time grouping by its unit at the top level', () => {
    const months = testProjection({
      view: view({
        mode: 'aggregate',
        grouping: 'time',
        time_granularity: 'month',
        root: root('aggregate', 'time', 'month'),
      }),
      breadcrumbs: [{ dimension: 'time', label: 'Months' }],
    })
    expect(breadcrumbTrail(months)).toEqual([{ label: 'Months', current: true }])
  })

  it('names a time drill by its period and its root by the unit it returns to', () => {
    const drilled = testProjection({
      view: view({
        mode: 'detail',
        grouping: 'time',
        time_granularity: 'day',
        drill_depth: 1,
        root: root('aggregate', 'time', 'month'),
      }),
      breadcrumbs: [{ dimension: 'time', label: 'Sep 2026' }],
    })
    expect(breadcrumbTrail(drilled)).toEqual([
      { label: 'Months', depth: 0, current: false },
      { label: 'Sep 2026', current: true },
    ])
  })

  it('names the root by the unit the server reports, even for a currency-scoped drill', () => {
    const drilled = testProjection({
      view: view({
        mode: 'detail',
        grouping: 'time',
        time_granularity: 'day',
        drill_depth: 1,
        root: root('aggregate', 'time', 'year'),
      }),
      breadcrumbs: [{ dimension: 'time', label: 'Sep 2026' }],
    })
    expect(breadcrumbTrail(drilled)[0]).toEqual({ label: 'Years', depth: 0, current: false })
  })

  it('breaks the d view down by time with All transactions as the root', () => {
    const detail = testProjection({
      view: view({
        mode: 'detail',
        grouping: 'merchant',
        sub_grouping: 'time',
        root: root('detail', 'merchant'),
      }),
      breadcrumbs: [{ dimension: 'merchant', label: 'Transactions' }],
    })
    expect(breadcrumbTrail(detail)).toEqual([
      { label: 'All transactions', depth: 0, current: false },
      { label: 'by Year', current: true },
    ])
  })

  it('names the root after the view a drill returns to, such as the d view', () => {
    const month = testProjection({
      view: view({
        mode: 'detail',
        grouping: 'merchant',
        drill_depth: 1,
        root: root('detail', 'merchant'),
      }),
      breadcrumbs: [{ dimension: 'time', label: 'Sep 2026' }],
    })
    expect(breadcrumbTrail(month)[0]).toEqual({
      label: 'All transactions',
      depth: 0,
      current: false,
    })
  })

  it('names the root after the grouping the drill was opened from', () => {
    const drilled = testProjection({
      view: view({
        mode: 'detail',
        grouping: 'category',
        drill_depth: 1,
        root: root('aggregate', 'category'),
      }),
      breadcrumbs: [{ dimension: 'category', label: 'Groceries' }],
    })
    expect(breadcrumbTrail(drilled)[0]).toEqual({ label: 'Categories', depth: 0, current: false })
  })
})

describe('breakDownOptions', () => {
  it('is empty at the top level', () => {
    expect(breakDownOptions(topLevel)).toEqual([])
  })

  it('lists Transactions, then the dimensions the drill path has not used', () => {
    expect(breakDownOptions(depthOne).map((option) => option.label)).toEqual([
      'Transactions',
      'Category',
      'Group',
      'Account',
      'Time',
    ])
    expect(breakDownOptions(depthTwo).map((option) => option.value)).toEqual([
      'transactions',
      'group',
      'account',
      'time',
    ])
  })

  it('names the unit of the chosen Time breakdown', () => {
    const subGrouped = {
      ...depthOne,
      view: { ...depthOne.view, sub_grouping: 'time', time_granularity: 'day' },
    }
    expect(breakDownOptions(subGrouped).at(-1)).toEqual({
      value: 'time',
      label: 'Time, Day',
      text: 'Time · Day',
    })
    const unchosen = { ...depthOne, view: { ...depthOne.view, time_granularity: 'day' } }
    expect(breakDownOptions(unchosen).at(-1)).toEqual({ value: 'time', label: 'Time' })
  })
})
