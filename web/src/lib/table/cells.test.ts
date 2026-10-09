import { describe, expect, it } from 'vitest'

import { contextCell, incomeDivider, moneyCell, netBar, savingsCell, shareCell } from './cells'
import type { ViewProjection } from '../api/client'
import { testAggregateRow, testMoney, testProjection } from '../../test/projection'

describe('moneyCell', () => {
  it('shows In and Out zeros as a dash', () => {
    expect(moneyCell(testMoney('0', '+0.00'), '—')).toBe('—')
  })

  it('shows a zero Net without its sign', () => {
    expect(moneyCell(testMoney('0', '+0.00'), 'unsigned')).toBe('0.00')
  })

  it('keeps the server display for every other amount', () => {
    expect(moneyCell(testMoney('123456', '+1,234.56'), '—')).toBe('+1,234.56')
    expect(moneyCell(testMoney('-500', '-5.00'), 'unsigned')).toBe('-5.00')
  })
})

describe('shareCell', () => {
  it('formats tenths of a percent and a 0..1 bar ratio', () => {
    expect(shareCell(testAggregateRow({ share_tenths: 62 }))).toEqual({
      label: '6.2%',
      ratio: 0.062,
    })
    expect(shareCell(testAggregateRow({ share_tenths: 1000 }))).toEqual({
      label: '100.0%',
      ratio: 1,
    })
  })

  it('shows a dash when Net is zero', () => {
    const row = testAggregateRow({ total: testMoney('0', '+0.00'), share_tenths: 0 })
    expect(shareCell(row)).toEqual({ label: '—', ratio: 0 })
  })
})

describe('contextCell', () => {
  it('shows the top category and its percent for merchants', () => {
    const row = testAggregateRow({ top_category: 'Groceries', top_category_percent: 82 })
    expect(contextCell(row, 'merchant')).toBe('Groceries 82%')
  })

  it('shows the group for categories and nothing elsewhere', () => {
    const row = testAggregateRow({ dimension: 'category', group: 'Food' })
    expect(contextCell(row, 'category')).toBe('Food')
    expect(contextCell(testAggregateRow({ dimension: 'category' }), 'category')).toBe('')
    expect(contextCell(testAggregateRow(), 'merchant')).toBe('')
    expect(contextCell(row, 'account')).toBe('')
  })
})

describe('savingsCell', () => {
  it('formats tenths of a percent, including negative rates', () => {
    expect(savingsCell(testAggregateRow({ savings_rate_tenths: 215 }))).toBe('21.5%')
    expect(savingsCell(testAggregateRow({ savings_rate_tenths: -37 }))).toBe('-3.7%')
    expect(savingsCell(testAggregateRow({ savings_rate_tenths: 0 }))).toBe('0.0%')
  })

  it('shows a dash when the server sends no rate', () => {
    expect(savingsCell(testAggregateRow())).toBe('—')
  })
})

describe('netBar', () => {
  it('extends from the center by half the width at the largest Net', () => {
    expect(netBar(10000)).toEqual({ side: 'in', percent: 50 })
    expect(netBar(-5000)).toEqual({ side: 'out', percent: 25 })
  })

  it('draws nothing for a zero or missing ratio', () => {
    expect(netBar(0)).toBeNull()
    expect(netBar(undefined)).toBeNull()
  })
})

describe('incomeDivider', () => {
  const net = (minor: string, index = 0) =>
    testAggregateRow({
      index,
      identity: `agg-${index}`,
      total: testMoney(minor, minor.startsWith('-') ? minor : `+${minor}`),
    })
  /** A loaded window of rows sorted by Net, starting at `offset`. */
  const loaded = (offset: number, minors: string[]) =>
    testProjection({
      detail_rows: null,
      aggregate_rows: minors.map((minor, position) => net(minor, offset + position)),
      total_rows: 400,
      window: { offset, limit: 200, count: minors.length },
    })
  const dividers = (windows: ViewProjection[], indexes: number[]) =>
    indexes.map((index) => incomeDivider(windows, index))

  it('marks where Spending starts when Net sorts descending', () => {
    const windows = [loaded(0, ['500', '100', '0', '-100', '-900'])]
    expect(dividers(windows, [0, 1, 2, 3, 4])).toEqual([null, null, null, 'Spending', null])
  })

  it('marks where Income starts when Net sorts ascending', () => {
    const windows = [loaded(0, ['-900', '-100', '100', '500'])]
    expect(dividers(windows, [0, 1, 2, 3])).toEqual([null, null, 'Income', null])
  })

  it('draws no divider on the first row of the result', () => {
    expect(incomeDivider([loaded(0, ['100', '200'])], 0)).toBeNull()
  })

  it('compares a window’s first row with the cached last row of the window before it', () => {
    const before = loaded(0, [...Array<string>(199).fill('-500'), '-100'])
    const current = loaded(200, ['100', '200'])
    expect(incomeDivider([current, before], 200)).toBe('Income')
    expect(incomeDivider([current, before], 201)).toBeNull()
  })

  it('draws no divider at a window edge until the window before it is loaded', () => {
    expect(incomeDivider([loaded(200, ['100', '200'])], 200)).toBeNull()
  })
})
