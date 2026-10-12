import { describe, expect, it } from 'vitest'

import { ariaKeyshortcuts, navItems, type NavItem } from './nav-items'
import type { ViewProjection } from '../api/client'
import { navCapabilityIDs, testCapabilities } from '../../test/capabilities'
import { testProjection } from '../../test/projection'

function capabilities(unavailable: string[] = []) {
  return testCapabilities(navCapabilityIDs, unavailable)
}

function aggregate(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return testProjection({
    view: {
      mode: 'aggregate',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
      sort_fields: ['merchant', 'count', 'amount'],
      drill_depth: 0,
      root: { mode: 'aggregate', grouping: 'merchant', time_granularity: 'year' },
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
      { kind: 'local', action: 'manage.groups' },
    ])
  })

  it('lists Maintain entries and adds Amazon import only on Amazon profiles', () => {
    expect(summary(navItems(aggregate()).maintain)).toEqual([
      { id: 'duplicates', label: 'Duplicates', key: 'D', active: false, available: true },
      { id: 'export', label: 'Export', key: 'E', active: false, available: true },
      { id: 'categories', label: 'Edit categories', key: 'C', active: false, available: true },
      { id: 'groups', label: 'Edit groups', key: 'G', active: false, available: true },
    ])
    const amazon = navItems(aggregate({ profile_kind: 'amazon' })).maintain.at(-1)
    expect(amazon).toEqual({
      id: 'amazon',
      label: 'Import Amazon orders',
      key: 'r',
      active: false,
      available: true,
      reason: undefined,
      run: { kind: 'local', action: 'provider.refresh' },
    })
  })

  it('marks the active grouping inside a drill and All transactions only at top level', () => {
    const drilled = aggregate({
      view: { ...aggregate().view, mode: 'detail', grouping: 'category', drill_depth: 1 },
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

  it('disables entries whose capability is unavailable or missing', () => {
    const projection = aggregate({
      capabilities: capabilities(['view.set-grouping', 'category-group.manage']).filter(
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
      'groups',
    ])
  })

  it('explains why an entry is unavailable', () => {
    const projection = aggregate({
      capabilities: [
        ...capabilities(['view.find-duplicates', 'transactions.export']).filter(
          ({ id }) => id !== 'transactions.export',
        ),
        {
          id: 'transactions.export',
          description: 'Export',
          category: 'Actions',
          available: false,
          reason: 'Nothing to export.',
        },
      ],
    })
    const reasons = Object.fromEntries(
      navItems(projection).maintain.map(({ id, reason }) => [id, reason]),
    )
    expect(reasons).toEqual({
      duplicates: 'Not available in this view',
      export: 'Nothing to export.',
      categories: undefined,
      groups: undefined,
    })
  })
})

describe('ariaKeyshortcuts', () => {
  it('names Shift for uppercase keys and leaves other keys as shown', () => {
    expect(ariaKeyshortcuts('D')).toBe('Shift+D')
    expect(ariaKeyshortcuts('G')).toBe('Shift+G')
    expect(ariaKeyshortcuts('d')).toBe('d')
    expect(ariaKeyshortcuts('1')).toBe('1')
  })
})
