import { describe, expect, it } from 'vitest'

import { pendingParts, pendingText } from './pending'

describe('pendingText', () => {
  it('counts pending edits and affected transactions', () => {
    expect(
      pendingText({ active_operations: 3, inactive_operations: 0, affected_transactions: 27 }),
    ).toBe('3 pending edits · 27 transactions')
  })

  it('uses singular nouns for one edit and one transaction', () => {
    expect(
      pendingText({ active_operations: 1, inactive_operations: 0, affected_transactions: 1 }),
    ).toBe('1 pending edit · 1 transaction')
  })

  it('reports zero pending edits', () => {
    expect(
      pendingText({ active_operations: 0, inactive_operations: 0, affected_transactions: 0 }),
    ).toBe('0 pending edits · 0 transactions')
  })

  it('lists the phrases separately', () => {
    expect(
      pendingParts({ active_operations: 2, inactive_operations: 1, affected_transactions: 7 }),
    ).toEqual(['2 pending edits', '7 transactions', '1 redo'])
  })

  it('adds the redo count when redo history exists', () => {
    expect(
      pendingText({ active_operations: 2, inactive_operations: 1, affected_transactions: 7 }),
    ).toBe('2 pending edits · 7 transactions · 1 redo')
  })
})
