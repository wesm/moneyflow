import { describe, expect, it } from 'vitest'

import { breakDownOptions, breadcrumbTrail } from './breadcrumbs'
import { testProjection } from '../../test/projection'
import type { ViewProjection } from '../api/client'

const merchantDrill = 'drill=merchant%3AUSD%3A2%3Aexample-key'
const categoryDrill = 'drill=category%3AUSD%3A2%3Aexample-category'
const timeDrill = 'drill=time%3AUSD%3A2%3Amonth%3A2026-09'
const base = testProjection()

function view(overrides: Partial<ViewProjection['view']>): ViewProjection['view'] {
  return { ...base.view, ...overrides }
}

const topLevel = testProjection({
  canonical_query: 'v=1',
  view: view({ mode: 'aggregate', grouping: 'merchant' }),
  breadcrumbs: [{ dimension: 'merchant', label: 'Merchants' }],
})
const depthOne = testProjection({
  canonical_query: `v=1&mode=detail&${merchantDrill}`,
  view: view({ mode: 'detail', grouping: 'merchant' }),
  breadcrumbs: [{ dimension: 'merchant', label: 'Corner Grocery' }],
})
const depthTwo = testProjection({
  canonical_query: `v=1&mode=detail&${merchantDrill}&${categoryDrill}`,
  view: view({ mode: 'detail', grouping: 'merchant' }),
  breadcrumbs: [
    { dimension: 'merchant', label: 'Corner Grocery' },
    { dimension: 'category', label: 'Groceries' },
  ],
})

describe('breadcrumbTrail', () => {
  it('names the grouping at the top level, as the current page', () => {
    expect(breadcrumbTrail(topLevel)).toEqual([{ label: 'Merchants', current: true }])
    const categories = testProjection({
      canonical_query: 'v=1&group=category',
      view: view({ mode: 'aggregate', grouping: 'category' }),
      breadcrumbs: [{ dimension: 'category', label: 'Categories' }],
    })
    expect(breadcrumbTrail(categories)).toEqual([{ label: 'Categories', current: true }])
  })

  it('names the d view All transactions', () => {
    const detail = testProjection({
      canonical_query: 'v=1&mode=detail',
      view: view({ mode: 'detail', grouping: 'account' }),
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
    const subGrouped = { ...depthOne, view: view({ mode: 'detail', sub_grouping: 'category' }) }
    expect(breadcrumbTrail(subGrouped)).toEqual([
      { label: 'Merchants', depth: 0, current: false },
      { label: 'Corner Grocery', depth: 1, current: false },
      { label: 'by Category', current: true },
    ])
  })

  it('names time grouping by its unit at the top level', () => {
    const months = testProjection({
      canonical_query: 'v=1&group=time&time=month',
      view: view({ mode: 'aggregate', grouping: 'time', time_granularity: 'month' }),
      breadcrumbs: [{ dimension: 'time', label: 'Months' }],
    })
    expect(breadcrumbTrail(months)).toEqual([{ label: 'Months', current: true }])
  })

  it('names a time drill by its period and its root by the unit it returns to', () => {
    const months = encodeURIComponent('navigation:group=time&time=month')
    const drilled = testProjection({
      canonical_query: `v=1&group=time&mode=detail&${timeDrill}&return=${months}`,
      view: view({ mode: 'detail', grouping: 'time', time_granularity: 'day' }),
      breadcrumbs: [{ dimension: 'time', label: 'Sep 2026' }],
    })
    expect(breadcrumbTrail(drilled)).toEqual([
      { label: 'Months', depth: 0, current: false },
      { label: 'Sep 2026', current: true },
    ])
  })

  it('names the root Time when the unit it returns to is not in the link', () => {
    const scoped = encodeURIComponent('navigation:cur=USD%3A2&group=time')
    const drilled = testProjection({
      canonical_query: `v=1&cur=USD%3A2&group=time&mode=detail&${timeDrill}&return=${scoped}`,
      view: view({ mode: 'detail', grouping: 'time', time_granularity: 'day' }),
      breadcrumbs: [{ dimension: 'time', label: 'Sep 2026' }],
    })
    expect(breadcrumbTrail(drilled)[0]).toEqual({ label: 'Time', depth: 0, current: false })
    const linked = testProjection({
      canonical_query: `v=1&group=time&mode=detail&${timeDrill}`,
      view: view({ mode: 'detail', grouping: 'time', time_granularity: 'day' }),
      breadcrumbs: [{ dimension: 'time', label: 'Sep 2026' }],
    })
    expect(breadcrumbTrail(linked)[0]).toEqual({ label: 'Time', depth: 0, current: false })
  })

  it('breaks the d view down by time with All transactions as the root', () => {
    const detail = testProjection({
      canonical_query: 'v=1&mode=detail&subgroup=time',
      view: view({ mode: 'detail', grouping: 'merchant', sub_grouping: 'time' }),
      breadcrumbs: [{ dimension: 'merchant', label: 'Transactions' }],
    })
    expect(breadcrumbTrail(detail)).toEqual([
      { label: 'All transactions', depth: 0, current: false },
      { label: 'by Time', current: true },
    ])
  })

  it('names the root after the view a drill returns to, such as the d view', () => {
    const detail = encodeURIComponent('subgroup:mode=detail')
    const byTime = encodeURIComponent('navigation:mode=detail&subgroup=time')
    const month = testProjection({
      canonical_query: `v=1&mode=detail&${timeDrill}&return=${detail}&return=${byTime}`,
      view: view({ mode: 'detail', grouping: 'merchant' }),
      breadcrumbs: [{ dimension: 'time', label: 'Sep 2026' }],
    })
    expect(breadcrumbTrail(month)[0]).toEqual({
      label: 'All transactions',
      depth: 0,
      current: false,
    })
  })

  it('names the root after the grouping the drill was opened from', () => {
    const categories = encodeURIComponent('navigation:group=category')
    const drilled = testProjection({
      canonical_query: `v=1&group=category&mode=detail&${categoryDrill}&return=${categories}`,
      view: view({ mode: 'detail', grouping: 'category' }),
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
})
