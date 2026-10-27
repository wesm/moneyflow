import { describe, expect, it, vi } from 'vitest'

import type { InsightsResponse } from '../api/client'
import { testAggregateRow, testProjection } from '../../test/projection'
import { testInsights } from '../../test/insights'
import {
  compositionBars,
  moveToRow,
  periodIndex,
  periodShare,
  type Composition,
} from './composition'

type Context = InsightsResponse['ranges']['context']

const composition: Composition = {
  spending: [
    { key: 'k1', identity: 'merchant:rent', label: 'Rent', net: '-200000', share_tenths: 400 },
    {
      key: 'k2',
      identity: 'merchant:grocer',
      label: 'Corner Grocer',
      net: '-36000',
      share_tenths: 62,
    },
    { label: '5 others', net: '-100000', share_tenths: 200, others: 5 },
  ],
  income: [
    { key: 'k3', identity: 'merchant:pay', label: 'Payroll', net: '841072', share_tenths: 1000 },
  ],
}

describe('compositionBars', () => {
  it('lists one side with shares, exact Net, and the composition bar name', () => {
    const bars = compositionBars(composition, 'spending', 2)
    expect(bars.map((bar) => bar.name)).toEqual([
      'Rent, -2,000.00, 40.0%',
      'Corner Grocer, -360.00, 6.2%',
      '5 others, -1,000.00, 20.0%',
    ])
    expect(bars[1]).toMatchObject({
      key: 'merchant:grocer',
      identity: 'merchant:grocer',
      label: 'Corner Grocer',
      share: '6.2%',
      net: '-360.00',
      others: false,
    })
    expect(compositionBars(composition, 'income', 2).map((bar) => bar.label)).toEqual(['Payroll'])
  })

  it('sizes bars by share against the largest one', () => {
    const bars = compositionBars(composition, 'spending', 2)
    expect(bars.map((bar) => bar.length)).toEqual([10000, 1550, 5000])
  })

  it('folds the rest into an others bar that names no row', () => {
    const others = compositionBars(composition, 'spending', 2).at(-1)
    expect(others).toMatchObject({ key: 'others', others: true })
    expect(others?.identity).toBeUndefined()
  })

  it('keys period-composition entries, which name groups, not rows', () => {
    const groups: Composition = {
      spending: [{ key: 'food', label: 'Food', net: '-5000', share_tenths: 1000 }],
      income: [],
    }
    const [bar] = compositionBars(groups, 'spending', 2)
    expect(bar).toMatchObject({ key: 'food', others: false })
    expect(bar?.identity).toBeUndefined()
    expect(compositionBars(groups, 'income', 2)).toEqual([])
  })
})

describe('moveToRow', () => {
  function controller() {
    return {
      neighbors: [
        testProjection({
          aggregate_rows: [testAggregateRow({ identity: 'merchant:b', index: 200 })],
        }),
      ],
      moveCursorTo: vi.fn(() => Promise.resolve()),
      showAnchor: vi.fn(() => Promise.resolve()),
    }
  }
  const projection = testProjection({
    aggregate_rows: [testAggregateRow({ identity: 'merchant:a', index: 0 })],
  })

  it('moves the cursor at once to a row of the loaded window or a cached neighbor', async () => {
    const mover = controller()
    await moveToRow(mover, projection, 'merchant:a')
    await moveToRow(mover, projection, 'merchant:b')
    expect(mover.moveCursorTo.mock.calls).toEqual([[0], [200]])
    expect(mover.showAnchor).not.toHaveBeenCalled()
  })

  it('loads the window holding a row no loaded window has', async () => {
    const mover = controller()
    await moveToRow(mover, projection, 'merchant:z')
    expect(mover.showAnchor).toHaveBeenCalledWith('merchant:z')
    expect(mover.moveCursorTo).not.toHaveBeenCalled()
  })
})

describe('periodIndex', () => {
  const context: Context = [8, 9].map((month) => ({
    start: `2026-0${month}-01`,
    end: `2026-0${month}-28`,
    partial: false,
    absent: false,
    period: { granularity: 'month', year: 2026, month },
  }))

  it('finds the chart-context period of a Time row', () => {
    expect(periodIndex(context, { granularity: 'month', year: 2026, month: 9 })).toBe(1)
  })

  it('reports a period outside the chart context', () => {
    expect(periodIndex(context, { granularity: 'month', year: 2026, month: 10 })).toBeUndefined()
    expect(periodIndex(context, { granularity: 'year', year: 2026 })).toBeUndefined()
    expect(periodIndex(context, undefined)).toBeUndefined()
  })
})

describe('periodShare', () => {
  function timeInsights(granularity: 'day' | 'month'): InsightsResponse {
    const response = testInsights()
    response.ranges.context = [14, 15].map((day) => ({
      start: `2026-09-${day}`,
      end: `2026-09-${day}`,
      partial: false,
      absent: false,
      period:
        granularity === 'day'
          ? { granularity, year: 2026, month: 9, day }
          : { granularity, year: 2026, month: day - 6 },
    }))
    response.summary!.cashflow.labels = ['First', 'Second']
    response.summary!.period_composition = ['Food', 'Travel'].map((label) => ({
      spending: [{ key: label, label, net: '-100', share_tenths: 1000 }],
      income: [],
    }))
    return response
  }

  it("picks the cursor period's composition and names a day period by its date", () => {
    const response = timeInsights('day')
    const share = periodShare(response, { granularity: 'day', year: 2026, month: 9, day: 15 })
    expect(share?.label).toBe('Sep 15, 2026')
    expect(share?.composition.spending[0]?.label).toBe('Travel')
  })

  it("names a month period with the server's label", () => {
    const share = periodShare(timeInsights('month'), { granularity: 'month', year: 2026, month: 8 })
    expect(share?.label).toBe('First')
  })

  it('shows the last period when the cursor period is not in the chart context', () => {
    const share = periodShare(timeInsights('day'), undefined)
    expect(share?.label).toBe('Sep 15, 2026')
  })

  it('has nothing to show without period compositions, and rejects misaligned ones', () => {
    expect(periodShare(testInsights(), undefined)).toBeUndefined()
    const response = timeInsights('day')
    response.summary!.period_composition!.pop()
    expect(() => periodShare(response, undefined)).toThrow(/1 compositions for 2/)
  })
})
