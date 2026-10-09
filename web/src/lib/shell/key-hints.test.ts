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

  it('offers only closing while an overlay is open', () => {
    expect(keyHints(projection({ view: view('aggregate') }), true)).toEqual([
      { key: 'Esc', label: 'Close' },
    ])
  })

  it('never returns more than six hints', () => {
    for (const mode of ['aggregate', 'detail'] as const) {
      for (const selection_count of [0, 1]) {
        const hints = keyHints(projection({ view: view(mode), selection_count }), false)
        expect(hints.length).toBeLessThanOrEqual(6)
      }
    }
  })
})
