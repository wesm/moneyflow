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

  it('ends a sub-grouped drill with the breakdown, so the frame returns to its transactions', () => {
    const subGrouped = { ...depthOne, view: view({ mode: 'detail', sub_grouping: 'category' }) }
    expect(breadcrumbTrail(subGrouped)).toEqual([
      { label: 'Merchants', depth: 0, current: false },
      { label: 'Corner Grocery', depth: 1, current: false },
      { label: 'by Category', current: true },
    ])
  })

  it('labels a time drill with its period under Time', () => {
    const time = testProjection({
      canonical_query: `v=1&group=time&mode=detail&${timeDrill}`,
      view: view({ mode: 'detail', grouping: 'time' }),
      breadcrumbs: [{ dimension: 'time', label: 'Sep 2026' }],
    })
    expect(breadcrumbTrail(time)).toEqual([
      { label: 'Time', depth: 0, current: false },
      { label: 'Sep 2026', current: true },
    ])
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
