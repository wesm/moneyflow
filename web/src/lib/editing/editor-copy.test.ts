import { describe, expect, it } from 'vitest'

import {
  countLine,
  editorIntent,
  impactLines,
  matchChoice,
  scopeOptions,
  stageLabel,
} from './editor-copy'
import type { MerchantPreview } from '../api/client'
import { testAggregateRow, testProjection } from '../../test/projection'

function preview(overrides: Partial<MerchantPreview> = {}): MerchantPreview {
  return {
    version: '1',
    revision: '1',
    affected_transactions: 141,
    outside_view: 0,
    outside_currencies: [],
    transactions: [],
    ...overrides,
  }
}

const choices = [
  {
    id: 'merchant-a',
    label: 'Corner Grocery',
    protected: false,
    transaction_count: 130,
    in_use: true,
  },
  { id: 'merchant-b', label: 'Maple Court', protected: false, transaction_count: 34, in_use: true },
]

describe('countLine', () => {
  it('counts selected rows while a selection exists', () => {
    expect(countLine(testProjection({ selection_count: 3 }), 'row-0')).toBe('· 3 selected')
  })

  it('counts one transaction for a transaction row', () => {
    expect(countLine(testProjection(), 'row-0')).toBe('· 1 transaction')
  })

  it("counts a grouped row's transactions", () => {
    const projection = testProjection({
      detail_rows: null,
      aggregate_rows: [testAggregateRow({ identity: 'agg-0', count: 141 })],
    })
    expect(countLine(projection, 'agg-0')).toBe('· 141 transactions')
    expect(countLine(projection, 'missing')).toBe('')
  })
})

describe('scopeOptions', () => {
  it('offers both scopes with their counts once known', () => {
    expect(scopeOptions({ entity: true }, { transactions: 34, entity: 141 }, false)).toEqual([
      { value: 'transactions', label: '34 transactions' },
      { value: 'entity', label: 'Whole merchant · 141 transactions', disabled: false },
    ])
  })

  it('names selected transactions and drops counts still loading', () => {
    expect(scopeOptions({ entity: true }, {}, true)).toEqual([
      { value: 'transactions', label: 'Selected transactions' },
      { value: 'entity', label: 'Whole merchant', disabled: false },
    ])
    expect(scopeOptions({ entity: true }, { transactions: 1 }, true)[0]?.label).toBe(
      '1 selected transaction',
    )
  })

  it('disables the whole merchant with its reason as the title', () => {
    const reason = 'Clear the selection to rename the whole merchant'
    expect(scopeOptions({ entity: false, reason }, { transactions: 3 }, true)[1]).toEqual({
      value: 'entity',
      label: 'Whole merchant',
      disabled: true,
      title: reason,
    })
  })
})

describe('impactLines', () => {
  it('states the whole-merchant count and nothing else when all of it is in view', () => {
    expect(impactLines(preview(), 'entity', undefined, 'USD')).toEqual([
      'Changes all 141 transactions of this merchant.',
    ])
  })

  it('says how many are outside the view and names other currencies', () => {
    const outside = preview({
      outside_view: 127,
      outside_currencies: [
        { currency: 'EUR', scale: 2 },
        { currency: 'USD', scale: 2 },
      ],
    })
    expect(impactLines(outside, 'entity', undefined, 'USD')[1]).toBe(
      '127 of these are outside the current view (other periods, filters, or EUR).',
    )
    const sameCurrency = preview({
      outside_view: 1,
      outside_currencies: [{ currency: 'USD', scale: 2 }],
    })
    expect(impactLines(sameCurrency, 'entity', undefined, 'USD')[1]).toBe(
      '1 of these is outside the current view (other periods or filters).',
    )
  })

  it('names the merchant a rename merges into, for either scope', () => {
    expect(impactLines(preview(), 'transactions', 'Corner Grocery', 'USD')).toEqual([
      'Merges into Corner Grocery',
    ])
    expect(impactLines(preview(), 'entity', 'Corner Grocery', 'USD')).toContain(
      'Merges into Corner Grocery',
    )
  })

  it('says nothing for a transaction rename before the preview loads', () => {
    expect(impactLines(undefined, 'transactions', undefined, 'USD')).toEqual([])
  })
})

describe('stageLabel', () => {
  it('names the affected count, singular for one', () => {
    expect(stageLabel(3)).toBe('Stage change to 3 transactions')
    expect(stageLabel(1)).toBe('Stage change to 1 transaction')
    expect(stageLabel(undefined)).toBe('Stage change')
  })
})

describe('matchChoice', () => {
  it('matches a label exactly, ignoring case and surrounding space', () => {
    expect(matchChoice(choices, '  corner GROCERY ')?.id).toBe('merchant-a')
    expect(matchChoice(choices, 'Corner')).toBeUndefined()
    expect(matchChoice(choices, '')).toBeUndefined()
  })
})

describe('editorIntent', () => {
  const target = { kind: 'transaction', identity: 'row-0' }

  it('renames transactions to a new merchant with a fresh ID', () => {
    const intent = editorIntent({
      kind: 'merchant',
      target,
      text: ' New Name ',
      scope: 'transactions',
    })
    expect(intent.action).toBe('transaction.edit-merchant')
    expect(intent.target).toBe(target)
    expect(intent.input.scope).toBe('transactions')
    expect(intent.input.label).toBe('New Name')
    expect(intent.input.destination_id).toMatch(/^merchant_[0-9a-f-]{36}$/)
  })

  it('merges into an existing merchant by its ID', () => {
    const match = choices[0]
    expect(
      editorIntent({ kind: 'merchant', target, text: 'corner grocery', scope: 'entity', match })
        .input,
    ).toEqual({ scope: 'entity', label: 'corner grocery', destination_id: 'merchant-a' })
  })

  it('renames the whole merchant in place when the name is new', () => {
    expect(
      editorIntent({ kind: 'merchant', target, text: 'Renamed', scope: 'entity' }).input,
    ).toEqual({ scope: 'entity', label: 'Renamed' })
  })

  it('moves transactions to an existing category', () => {
    const match = {
      id: 'category-a',
      label: 'Groceries',
      protected: false,
      transaction_count: 1,
      in_use: true,
    }
    const intent = editorIntent({ kind: 'category', target, text: 'groceries', scope: '', match })
    expect(intent).toEqual({
      action: 'transaction.edit-category',
      target,
      input: { scope: 'transactions', destination_id: 'category-a' },
    })
  })

  it('creates a category in the chosen group', () => {
    const intent = editorIntent({
      kind: 'category',
      target,
      text: ' Pet Care ',
      scope: '',
      groupId: 'group-a',
    })
    expect(intent.input).toEqual({
      scope: 'transactions',
      destination_id: expect.stringMatching(/^category_[0-9a-f-]{36}$/),
      label: 'Pet Care',
      group_id: 'group-a',
    })
  })
})
