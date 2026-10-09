import { describe, expect, it } from 'vitest'

import { doubleClickAction } from './pointer'

describe('doubleClickAction', () => {
  it('drills a grouped row from any cell but the selection box', () => {
    for (const column of ['name', 'count', 'net', undefined]) {
      expect(doubleClickAction('aggregate', column)).toBe('drill')
    }
    expect(doubleClickAction('aggregate', 'select')).toBeUndefined()
  })

  it('edits Merchant and Category on a transaction row and opens Info elsewhere', () => {
    expect(doubleClickAction('transaction', 'merchant')).toBe('merchant')
    expect(doubleClickAction('transaction', 'category')).toBe('category')
    for (const column of ['date', 'account', 'amount', 'flags', undefined]) {
      expect(doubleClickAction('transaction', column)).toBe('information')
    }
    expect(doubleClickAction('transaction', 'select')).toBeUndefined()
  })
})
