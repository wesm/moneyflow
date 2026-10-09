import { describe, expect, it } from 'vitest'

import { compactTapAction, doubleClickAction } from './pointer'

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

describe('compactTapAction', () => {
  it('toggles selection from the leading tap area on any row', () => {
    expect(compactTapAction('aggregate', 'select')).toBe('toggle')
    expect(compactTapAction('transaction', 'select')).toBe('toggle')
  })

  it('drills a grouped row from its body and from its › button', () => {
    for (const column of ['name', 'net', 'count', 'in', 'out', 'actions', undefined]) {
      expect(compactTapAction('aggregate', column)).toBe('drill')
    }
  })

  it('opens Info from a transaction row body and the actions sheet from its ⋯ button', () => {
    for (const column of ['merchant', 'amount', 'date', 'category', 'account', undefined]) {
      expect(compactTapAction('transaction', column)).toBe('information')
    }
    expect(compactTapAction('transaction', 'actions')).toBe('actions')
  })
})
