import { describe, expect, it } from 'vitest'

import { reviewRowChange } from './review-rows'

const row = {
  date: '2024-01-01',
  merchant: 'Old Merchant',
  category: 'Old Category',
  amount: { minor: '-100', currency: 'USD', scale: 2, decimal: '-1.00', display: '-1.00' },
  hidden: false,
}

function operation(type: string, after?: string) {
  return {
    operation_id: 'operation',
    type,
    label: 'Label',
    active: true,
    affected_count: 1,
    ...(after === undefined ? {} : { after }),
  }
}

describe('reviewRowChange', () => {
  it('names the row field an operation changes, from the row’s value to the new one', () => {
    for (const type of ['merchant.label', 'merchant.merge', 'merchant.reassign']) {
      expect(reviewRowChange(operation(type, 'New Merchant'), row), type).toEqual({
        field: 'merchant',
        before: 'Old Merchant',
        after: 'New Merchant',
      })
    }
    for (const type of ['category.assign', 'category.create', 'category.label']) {
      expect(reviewRowChange(operation(type, 'New Category'), row), type).toEqual({
        field: 'category',
        before: 'Old Category',
        after: 'New Category',
      })
    }
    for (const type of ['category.merge', 'category.delete']) {
      expect(reviewRowChange(operation(type, 'Other Category'), row)?.field, type).toBe('category')
    }
  })

  it('shows a hide or show as the row’s visibility', () => {
    expect(reviewRowChange(operation('transaction.hide-toggle', 'Hidden'), row)).toEqual({
      field: 'visibility',
      before: 'Visible',
      after: 'Hidden',
    })
    expect(
      reviewRowChange(operation('transaction.hide-toggle', 'Visible'), { ...row, hidden: true }),
    ).toEqual({ field: 'visibility', before: 'Hidden', after: 'Visible' })
  })

  it('shows no change for fields the row does not show or that keep their value', () => {
    for (const type of ['category.move', 'group.label', 'group.merge', 'transaction.delete']) {
      expect(reviewRowChange(operation(type, 'Elsewhere'), row), type).toBeUndefined()
    }
    expect(reviewRowChange(operation('merchant.label'), row)).toBeUndefined()
    expect(reviewRowChange(operation('merchant.label', 'Old Merchant'), row)).toBeUndefined()
  })
})
