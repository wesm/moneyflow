import { describe, expect, it } from 'vitest'

import { keyHints } from './key-hints'
import type { ViewProjection } from '../api/client'
import { testCapabilities } from '../../test/capabilities'
import { testProjection } from '../../test/projection'

const hintCapabilities = [
  'view.drill',
  'view.cycle-grouping',
  'overlay.search',
  'time.choose',
  'overlay.help',
  'transaction.edit-merchant',
  'transaction.edit-category',
  'selection.toggle',
  'transaction.show-info',
  'transaction.toggle-hidden',
  'transaction.delete',
  'view.back',
]

function capabilities(unavailable: string[] = []) {
  return testCapabilities(hintCapabilities, unavailable)
}

function view(mode: 'aggregate' | 'detail', sub_grouping?: string): ViewProjection['view'] {
  return {
    mode,
    grouping: 'merchant',
    time_granularity: 'year',
    sort_field: 'amount',
    sort_direction: 'desc',
    sort_fields: ['merchant', 'count', 'amount'],
    drill_depth: 0,
    root: { mode, grouping: 'merchant', time_granularity: 'year' },
    ...(sub_grouping === undefined ? {} : { sub_grouping }),
  }
}

function projection(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return testProjection({ capabilities: capabilities(), ...overrides })
}

describe('keyHints', () => {
  it('offers open, group, search, period, and help in a grouped view', () => {
    expect(keyHints(projection({ view: view('aggregate') }), false)).toEqual([
      { key: '↵', label: 'Open' },
      { key: 'g', label: 'Group' },
      { key: '/', label: 'Search' },
      { key: 't', label: 'Period' },
      { key: '?', label: 'Help' },
    ])
  })

  it('names g Break down inside a drill', () => {
    const hints = keyHints(projection({ view: view('aggregate', 'category') }), false)
    expect(hints.find(({ key }) => key === 'g')).toEqual({ key: 'g', label: 'Break down' })
  })

  it('offers row edits, selection, and information in a transaction view', () => {
    expect(keyHints(projection({ view: view('detail') }), false)).toEqual([
      { key: 'm', label: 'Merchant' },
      { key: 'c', label: 'Category' },
      { key: 'Space', label: 'Select' },
      { key: 'i', label: 'Info' },
      { key: '?', label: 'Help' },
    ])
  })

  it('offers bulk edits and clearing while rows are selected', () => {
    expect(keyHints(projection({ view: view('detail'), selection_count: 3 }), false)).toEqual([
      { key: 'm', label: 'Merchant' },
      { key: 'c', label: 'Category' },
      { key: 'h', label: 'Hide' },
      { key: 'x', label: 'Delete' },
      { key: 'Esc', label: 'Clear' },
    ])
  })

  it('omits unavailable and missing capabilities', () => {
    const grouped = projection({
      view: view('aggregate'),
      capabilities: capabilities(['view.drill']).filter(({ id }) => id !== 'time.choose'),
    })
    expect(keyHints(grouped, false).map(({ key }) => key)).toEqual(['g', '/', '?'])

    const selected = projection({
      view: view('aggregate'),
      selection_count: 2,
      capabilities: capabilities(['transaction.delete']),
    })
    expect(keyHints(selected, false).map(({ key }) => key)).toEqual(['m', 'c', 'h', 'Esc'])
  })

  it('keeps x while a grouped selection explains why it cannot delete', () => {
    const reason = 'Open the group to delete its transactions.'
    const selected = projection({
      view: view('aggregate'),
      selection_count: 2,
      capabilities: capabilities(['transaction.delete']).map((capability) =>
        capability.id === 'transaction.delete' ? { ...capability, reason } : capability,
      ),
    })
    expect(keyHints(selected, false).map(({ key }) => key)).toEqual(['m', 'c', 'h', 'x', 'Esc'])
  })

  it('offers no row keys over an empty period', () => {
    const currency = { currency: 'USD', scale: 2 }
    for (const mode of ['aggregate', 'detail'] as const) {
      const empty = projection({
        view: { ...view(mode), currency },
        currencies: [{ ...currency, count: 1, first: '2026-01-02', latest: '2026-10-07' }],
        filters: {
          show_hidden: false,
          show_transfers: false,
          date_range: { from: '2026-11-01', to: '2026-11-30' },
        },
        statistics: [],
      })
      expect(keyHints(empty, false).map(({ key }) => key)).toEqual(['g', '/', 't', '?'])
    }
  })

  it('offers only closing while an overlay is open', () => {
    expect(keyHints(projection({ view: view('aggregate') }), true)).toEqual([
      { key: 'Esc', label: 'Close' },
    ])
  })
})
