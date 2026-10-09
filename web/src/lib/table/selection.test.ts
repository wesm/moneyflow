import { describe, expect, it } from 'vitest'

import { rowActionButtons, selectionButtons, selectionText } from './selection'
import type { ViewProjection } from '../api/client'
import type { components } from '../api/schema'
import { testCapabilities } from '../../test/capabilities'
import { testMoney, testProjection } from '../../test/projection'

type Statistics = components['schemas']['Statistics']

const usd = { currency: 'USD', scale: 2 }

function summary(count: number, minor: string, display: string, currency = 'USD'): Statistics {
  const net = { ...testMoney(minor, display), currency }
  return { currency, scale: 2, count, in: net, out: net, net }
}

function grouped(overrides: Partial<ViewProjection['view']> = {}): ViewProjection['view'] {
  return {
    mode: 'aggregate',
    grouping: 'merchant',
    time_granularity: 'year',
    sort_field: 'amount',
    sort_direction: 'desc',
    sort_fields: ['merchant', 'count', 'amount'],
    currency: usd,
    ...overrides,
  }
}

function projection(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return testProjection({
    selection_count: 3,
    view: grouped(),
    selection_summary: [summary(127, '-421240', '-4,212.40')],
    ...overrides,
  })
}

describe('selectionText', () => {
  it('is null without a selection', () => {
    expect(selectionText(projection({ selection_count: 0, selection_summary: [] }))).toBeNull()
  })

  it('counts selected groups, their transactions, and their Net', () => {
    expect(selectionText(projection())).toBe('3 merchants · 127 transactions · -4,212.40')
  })

  it('uses the singular for one group and one transaction', () => {
    const one = projection({ selection_count: 1, selection_summary: [summary(1, '-500', '-5.00')] })
    expect(selectionText(one)).toBe('1 merchant · 1 transaction · -5.00')
  })

  it('names the groups after the current dimension', () => {
    const categories = projection({ view: grouped({ grouping: 'category' }) })
    expect(selectionText(categories)).toMatch(/^3 categories · /)
    const broken = projection({ view: grouped({ sub_grouping: 'account' }) })
    expect(selectionText(broken)).toMatch(/^3 accounts · /)
    const months = projection({ view: grouped({ grouping: 'time', time_granularity: 'month' }) })
    expect(selectionText(months)).toMatch(/^3 months · /)
    const year = projection({
      selection_count: 1,
      view: grouped({ grouping: 'time', time_granularity: 'year' }),
    })
    expect(selectionText(year)).toMatch(/^1 year · /)
  })

  it('counts only transactions in a transaction view', () => {
    const detail = projection({
      selection_count: 12,
      view: grouped({ mode: 'detail' }),
      selection_summary: [summary(12, '-38810', '-388.10')],
    })
    expect(selectionText(detail)).toBe('12 transactions · -388.10')
  })

  it('shows a zero Net without its sign', () => {
    const zero = projection({ selection_summary: [summary(4, '0', '+0.00')] })
    expect(selectionText(zero)).toBe('3 merchants · 4 transactions · 0.00')
  })

  it('appends the Net of other currencies with their codes', () => {
    const mixed = projection({
      selection_summary: [
        summary(2, '1000', '+10.00', 'EUR'),
        summary(127, '-421240', '-4,212.40'),
        summary(1, '-250', '-2.50', 'CAD'),
      ],
    })
    expect(selectionText(mixed)).toBe(
      '3 merchants · 127 transactions · -4,212.40 · +10.00 EUR · -2.50 CAD',
    )
  })

  it('leaves out the count and Net the summary does not hold for the active currency', () => {
    expect(selectionText(projection({ selection_summary: [] }))).toBe('3 merchants')
    const detail = projection({ view: grouped({ mode: 'detail' }), selection_summary: [] })
    expect(selectionText(detail)).toBe('3 transactions')
    const other = projection({ selection_summary: [summary(2, '1000', '+10.00', 'EUR')] })
    expect(selectionText(other)).toBe('3 merchants · +10.00 EUR')
  })
})

const editIDs = [
  'transaction.edit-merchant',
  'transaction.edit-category',
  'transaction.toggle-hidden',
  'transaction.delete',
]

describe('selectionButtons', () => {
  it('offers the edits and Clear with their keys', () => {
    const buttons = selectionButtons(projection({ capabilities: testCapabilities(editIDs) }))
    expect(buttons).toEqual([
      { action: 'edit.merchant', label: 'Merchant', key: 'm' },
      { action: 'edit.category', label: 'Category', key: 'c' },
      { action: 'edit.hide', label: 'Hide', key: 'h' },
      { action: 'edit.delete', label: 'Delete', key: 'x' },
      { action: 'clear', label: 'Clear', key: 'Esc' },
    ])
  })

  it('disables each unavailable edit with the server reason', () => {
    const reason = 'Finish the provider write before editing or refreshing.'
    const capabilities = testCapabilities(editIDs, editIDs).map((capability) => ({
      ...capability,
      reason,
    }))
    const buttons = selectionButtons(projection({ capabilities }))
    expect(buttons.filter((button) => button.reason === reason).map(({ key }) => key)).toEqual([
      'm',
      'c',
      'h',
      'x',
    ])
    expect(buttons.at(-1)).toEqual({ action: 'clear', label: 'Clear', key: 'Esc' })
  })

  it('disables an edit the view does not offer', () => {
    const buttons = selectionButtons(projection({ capabilities: [] }))
    expect(buttons.slice(0, 4).every((button) => button.reason !== undefined)).toBe(true)
  })
})

describe('rowActionButtons', () => {
  it('offers the edits and Info for one row, with the keys that run them on the cursor row', () => {
    const ids = [...editIDs, 'transaction.show-info']
    const buttons = rowActionButtons(
      projection({ selection_count: 0, capabilities: testCapabilities(ids) }),
    )
    expect(buttons).toEqual([
      { action: 'edit.merchant', label: 'Merchant', key: 'm' },
      { action: 'edit.category', label: 'Category', key: 'c' },
      { action: 'edit.hide', label: 'Hide', key: 'h' },
      { action: 'edit.delete', label: 'Delete', key: 'x' },
      { action: 'transaction.info', label: 'Info', key: 'i' },
    ])
  })

  it('gives an unavailable Delete the server reason and leaves the rest enabled', () => {
    const reason = 'Deleting is not available here.'
    const ids = [...editIDs, 'transaction.show-info']
    const capabilities = testCapabilities(ids, ['transaction.delete']).map((capability) =>
      capability.available ? capability : { ...capability, reason },
    )
    const buttons = rowActionButtons(projection({ selection_count: 0, capabilities }))
    expect(buttons.map((button) => button.reason)).toEqual([
      undefined,
      undefined,
      undefined,
      reason,
      undefined,
    ])
  })

  it('disables the edits while a selection exists, since they would edit the selection', () => {
    const reason = 'Deleting is not available here.'
    const ids = [...editIDs, 'transaction.show-info']
    const capabilities = testCapabilities(ids, ['transaction.delete']).map((capability) =>
      capability.available ? capability : { ...capability, reason },
    )
    const buttons = rowActionButtons(projection({ selection_count: 1, capabilities }))
    const selected = 'Clear the selection to edit one transaction'
    expect(buttons.map((button) => button.reason)).toEqual([
      selected,
      selected,
      selected,
      reason,
      undefined,
    ])
  })
})
