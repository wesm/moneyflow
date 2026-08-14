import { describe, expect, it } from 'vitest'

import { OwnedHistoryLedger, type MoneyflowHistoryState } from './history'
import type { SelectionValue } from '../api/client'

const selection = 'mfsel1.example' as SelectionValue

describe('owned browser history ledger', () => {
  it('returns a direct jump only across contiguous entries owned by this page instance', () => {
    const ledger = new OwnedHistoryLedger('instance-a')
    const first = state(0, 'v=1')
    const second = state(1, 'v=1&group=category')
    const third = state(2, 'v=1&group=category&drill=x')
    ledger.record(first)
    ledger.record(second)
    ledger.record(third)

    expect(ledger.deltaTo('v=1', selection, third)).toBe(-2)
    expect(ledger.deltaTo('v=1&group=category', selection, third)).toBe(-1)
  })

  it('rejects reloads, foreign state, sequence gaps, and missing targets', () => {
    const ledger = new OwnedHistoryLedger('instance-a')
    ledger.record(state(0, 'v=1'))
    ledger.record(state(2, 'v=1&group=category&drill=x'))

    expect(ledger.deltaTo('v=1', selection, state(2, 'v=1&group=category&drill=x'))).toBeUndefined()
    expect(
      ledger.deltaTo('missing', selection, state(2, 'v=1&group=category&drill=x')),
    ).toBeUndefined()
    expect(
      ledger.deltaTo('v=1', selection, {
        ...state(2, 'v=1&group=category&drill=x'),
        instance: 'other',
      }),
    ).toBeUndefined()
    expect(
      new OwnedHistoryLedger('instance-a').deltaTo('v=1', selection, state(0, 'v=1')),
    ).toBeUndefined()
  })

  it('does not jump to an owned query entry carrying stale selection state', () => {
    const ledger = new OwnedHistoryLedger('instance-a')
    const first = state(0, 'v=1')
    const second = state(1, 'v=1&search=coffee')
    ledger.record(first)
    ledger.record(second)

    const changedSelection = 'mfsel1.changed' as SelectionValue
    expect(ledger.deltaTo('v=1', changedSelection, second)).toBeUndefined()
  })

  it('accepts only exact owned history-state shapes', () => {
    const ledger = new OwnedHistoryLedger('instance-a')
    expect(ledger.read(state(0, 'v=1'))?.query).toBe('v=1')
    expect(ledger.read({ ...state(0, 'v=1'), owner: 'foreign' })).toBeUndefined()
    expect(ledger.read({ ...state(0, 'v=1'), cursorIndex: -1 })).toBeUndefined()
    expect(ledger.read(null)).toBeUndefined()
  })
})

function state(sequence: number, query: string): MoneyflowHistoryState {
  return {
    owner: 'moneyflow-web-v1',
    instance: 'instance-a',
    sequence,
    query,
    cursorIndex: 0,
    scrollTop: 0,
    selection,
  }
}
