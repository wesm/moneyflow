import { describe, expect, it } from 'vitest'

import { navItems, type NavItem } from './nav-items'
import type { ViewProjection } from '../api/client'
import { testProjection } from '../../test/projection'

type Capability = NonNullable<ViewProjection['capabilities']>[number]

const allCapabilities: Array<[string, string]> = [
  ['view.set-grouping', '1-5'],
  ['view.show-detail', 'd'],
  ['view.find-duplicates', 'D'],
  ['transactions.export', 'E'],
  ['category.manage', 'C'],
  ['category-group.manage', 'G'],
  ['provider.refresh', 'r'],
]

function capabilities(unavailable: string[] = []): Capability[] {
  return allCapabilities.map(([id, key_display]) => ({
    id,
    key_display,
    description: id,
    category: 'Views',
    available: !unavailable.includes(id),
  }))
}

function aggregate(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return testProjection({
    view: {
      mode: 'aggregate',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
    },
    canonical_query: 'v=1',
    capabilities: capabilities(),
    ...overrides,
  })
}

function summary(items: NavItem[]) {
  return items.map(({ id, label, key, active, available }) => ({
    id,
    label,
    key,
    active,
    available,
  }))
}

describe('navItems', () => {
  it('lists the Explore groupings and All transactions with their keys', () => {
    const { explore } = navItems(aggregate())
    expect(summary(explore)).toEqual([
      { id: 'merchant', label: 'Merchants', key: '1', active: true, available: true },
      { id: 'category', label: 'Categories', key: '2', active: false, available: true },
      { id: 'group', label: 'Groups', key: '3', active: false, available: true },
      { id: 'account', label: 'Accounts', key: '4', active: false, available: true },
      { id: 'time', label: 'Time', key: '5', active: false, available: true },
      { id: 'detail', label: 'All transactions', key: 'd', active: false, available: true },
    ])
  })

  it('runs the same actions as the keys', () => {
    const { explore, maintain } = navItems(aggregate())
    expect(explore.map(({ run }) => run)).toEqual([
      { kind: 'apply', action: 'view.set-grouping', dimension: 'merchant' },
      { kind: 'apply', action: 'view.set-grouping', dimension: 'category' },
      { kind: 'apply', action: 'view.set-grouping', dimension: 'group' },
      { kind: 'apply', action: 'view.set-grouping', dimension: 'account' },
      { kind: 'apply', action: 'view.set-grouping', dimension: 'time' },
      { kind: 'apply', action: 'view.show-detail' },
    ])
    expect(maintain.map(({ run }) => run)).toEqual([
      { kind: 'local', action: 'view.duplicates' },
      { kind: 'local', action: 'transactions.export' },
      { kind: 'local', action: 'manage.categories' },
    ])
  })

  it('lists Maintain entries and adds Amazon import only on Amazon profiles', () => {
    expect(summary(navItems(aggregate()).maintain)).toEqual([
      { id: 'duplicates', label: 'Duplicates', key: 'D', active: false, available: true },
      { id: 'export', label: 'Export', key: 'E', active: false, available: true },
      {
        id: 'taxonomy',
        label: 'Categories and groups',
        key: 'C/G',
        active: false,
        available: true,
      },
    ])
    const amazon = navItems(aggregate({ profile_kind: 'amazon' })).maintain.at(-1)
    expect(amazon).toEqual({
      id: 'amazon',
      label: 'Import Amazon orders',
      key: 'r',
      active: false,
      available: true,
      run: { kind: 'local', action: 'provider.refresh' },
    })
  })

  it('marks the active grouping inside a drill and All transactions only at top level', () => {
    const drilled = aggregate({
      view: { ...aggregate().view, mode: 'detail', grouping: 'category' },
      canonical_query: 'drill=category%3Aexample&mode=detail&v=1',
    })
    expect(
      navItems(drilled)
        .explore.filter(({ active }) => active)
        .map(({ id }) => id),
    ).toEqual(['category'])

    const allTransactions = aggregate({
      view: { ...aggregate().view, mode: 'detail', grouping: 'category' },
      canonical_query: 'mode=detail&v=1',
    })
    expect(
      navItems(allTransactions)
        .explore.filter(({ active }) => active)
        .map(({ id }) => id),
    ).toEqual(['detail'])
  })

  it('ignores drills recorded only in a return frame', () => {
    const projection = aggregate({
      view: { ...aggregate().view, mode: 'detail' },
      canonical_query: `mode=detail&return=${encodeURIComponent('search:drill=merchant%3Aa')}&v=1`,
    })
    expect(navItems(projection).explore.find(({ active }) => active)?.id).toBe('detail')
  })

  it('disables entries whose capability is unavailable or missing', () => {
    const projection = aggregate({
      capabilities: capabilities(['view.set-grouping', 'category.manage']).filter(
        ({ id }) => id !== 'transactions.export',
      ),
    })
    const { explore, maintain } = navItems(projection)
    expect(explore.filter(({ available }) => !available).map(({ id }) => id)).toEqual([
      'merchant',
      'category',
      'group',
      'account',
      'time',
    ])
    expect(maintain.filter(({ available }) => !available).map(({ id }) => id)).toEqual([
      'export',
      'taxonomy',
    ])
  })
})
