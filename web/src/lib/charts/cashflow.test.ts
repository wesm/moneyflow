import { describe, expect, it } from 'vitest'

import type { InsightsResponse, InsightsSeries, ViewProjection } from '../api/client'
import type { InsightsEntry } from '../insights/cache.svelte'
import { testInsights, testSeries } from '../../test/insights'
import { testAggregateRow, testProjection } from '../../test/projection'
import {
  cashflowCursor,
  cashflowGeometry,
  cursorGeometry,
  NET_DOT_RADIUS,
  rangeAnchor,
  rowAmounts,
  scopeFromColumns,
  tickEvery,
} from './cashflow'

type Ranges = InsightsResponse['ranges']
type Context = Ranges['context']

function month(year: number, number: number, partial = false): Context[number] {
  const start = `${year}-${String(number).padStart(2, '0')}-01`
  const last = new Date(Date.UTC(year, number, 0)).getUTCDate()
  const end = `${start.slice(0, 8)}${String(last).padStart(2, '0')}`
  return {
    start,
    end,
    partial,
    absent: false,
    period: { granularity: 'month', year, month: number },
  }
}

function year(value: number, partial = false): Context[number] {
  return {
    start: `${value}-01-01`,
    end: `${value}-12-31`,
    partial,
    absent: false,
    period: { granularity: 'year', year: value },
  }
}

function day(year: number, number: number, value: number): Context[number] {
  const date = `${year}-${String(number).padStart(2, '0')}-${String(value).padStart(2, '0')}`
  return {
    start: date,
    end: date,
    partial: false,
    absent: false,
    period: { granularity: 'day', year, month: number, day: value },
  }
}

interface Flow {
  labels: string[]
  in: string[]
  out: string[]
  net: string[]
}

function entry(context: Context, flow: Flow, ranges: Partial<Ranges> = {}): InsightsEntry {
  const response = testInsights()
  response.ranges = { ...response.ranges, context, ...ranges }
  response.summary!.cashflow = { ...flow, count: flow.in.map(() => 1) }
  return { query: response.query, revision: response.revision, response, series: new Map() }
}

/** Online Megastore across Aug and Sep 2026, the spec's worked example, scoped to September. */
function megastore(): InsightsEntry {
  return entry([month(2026, 8), month(2026, 9)], {
    labels: ['Aug 2026', 'Sep 2026'],
    in: ['0', '12000'],
    out: ['-30000', '-48000'],
    net: ['-30000', '-36000'],
  })
}

describe('cashflowGeometry', () => {
  it('draws In and Out on one magnitude scale with Net from the same zero', () => {
    // 4 px of room for the dots above and below leaves a 100 px plot. In peaks at 120.00 and
    // Out at 480.00, so the scale spans 600.00 and zero sits a fifth of the way down.
    const geometry = cashflowGeometry(megastore(), 200, 108)
    expect(geometry.zeroY).toBe(24)
    const [august, september] = geometry.columns
    expect(september).toMatchObject({
      x: 100,
      width: 100,
      inTop: 4,
      inHeight: 20,
      outHeight: 80,
      netY: 84,
      partial: false,
      inScope: true,
      name: 'Sep 2026, in +120.00, out -480.00, net -360.00',
    })
    expect(august).toMatchObject({
      x: 0,
      inTop: 24,
      inHeight: 0,
      outHeight: 50,
      netY: 74,
      inScope: false,
      name: 'Aug 2026, in +0.00, out -300.00, net -300.00',
    })
    expect(geometry.netPath).toBe('M50,74L150,84')
  })

  it('draws an all-zero context as zero columns on a flat Net line at the middle', () => {
    const geometry = cashflowGeometry(
      entry([month(2026, 8), month(2026, 9)], {
        labels: ['Aug 2026', 'Sep 2026'],
        in: ['0', '0'],
        out: ['0', '0'],
        net: ['0', '0'],
      }),
      200,
      108,
    )
    expect(geometry.zeroY).toBe(54)
    for (const column of geometry.columns) {
      expect(column).toMatchObject({ inTop: 54, inHeight: 0, outHeight: 0, netY: 54 })
    }
    expect(geometry.netPath).toBe('M50,54L150,54')
  })

  it('keeps a one-pixel sliver for amounts too small to round to a pixel', () => {
    const geometry = cashflowGeometry(
      entry([month(2026, 8), month(2026, 9)], {
        labels: ['Aug 2026', 'Sep 2026'],
        in: ['1', '0'],
        out: ['0', '-1000000'],
        net: ['1', '-1000000'],
      }),
      200,
      108,
    )
    expect(geometry.columns[0]).toMatchObject({ inHeight: 1, outHeight: 0 })
  })

  it('marks partial periods and bands only the columns that meet the scope', () => {
    const context = [month(2026, 8), month(2026, 9), month(2026, 10, true)]
    const flow = {
      labels: ['Aug 2026', 'Sep 2026', 'Oct 2026'],
      in: ['0', '0', '0'],
      out: ['0', '0', '0'],
      net: ['0', '0', '0'],
    }
    const custom = cashflowGeometry(
      entry(context, flow, { kind: 'custom', scope: { start: '2026-08-20', end: '2026-09-02' } }),
      300,
      100,
    )
    expect(custom.columns.map((column) => column.inScope)).toEqual([true, true, false])
    expect(custom.columns.map((column) => column.partial)).toEqual([false, false, true])

    // The Net line stops at the ring of the partial period's hollow dot.
    expect(custom.netPath).toBe(`M50,50L150,50L${250 - NET_DOT_RADIUS},50`)

    const everything = entry(context, flow, { kind: 'all' })
    delete everything.response.ranges.scope
    const all = cashflowGeometry(everything, 300, 100)
    expect(all.columns.some((column) => column.inScope)).toBe(false)
  })

  it('names day columns from their date and ticks them with the day number', () => {
    const geometry = cashflowGeometry(
      entry([day(2026, 9, 15)], {
        labels: ['2026-09-15'],
        in: ['0'],
        out: ['-48000'],
        net: ['-48000'],
      }),
      30,
      100,
    )
    expect(geometry.columns[0]).toMatchObject({
      label: 'Sep 15, 2026',
      tick: '15',
      name: 'Sep 15, 2026, in +0.00, out -480.00, net -480.00',
    })
  })

  it('ticks months with their short name and January with its year', () => {
    const geometry = cashflowGeometry(
      entry([month(2025, 12), month(2026, 1)], {
        labels: ['Dec 2025', 'Jan 2026'],
        in: ['0', '0'],
        out: ['0', '0'],
        net: ['0', '0'],
      }),
      200,
      100,
    )
    expect(geometry.columns.map((column) => column.tick)).toEqual(['Dec', '2026'])
  })

  it('draws a Year scope as one column per year and marks the scope year', () => {
    const context = [year(2024), year(2025), year(2026, true)]
    const geometry = cashflowGeometry(
      entry(
        context,
        {
          labels: ['2024', '2025', '2026'],
          in: ['0', '0', '12000'],
          out: ['0', '0', '-48000'],
          net: ['0', '0', '-36000'],
        },
        {
          kind: 'year',
          scope: { start: '2026-01-01', end: '2026-12-31' },
          context_granularity: 'year',
        },
      ),
      300,
      100,
    )
    expect(geometry.columns.map((column) => column.tick)).toEqual(['2024', '2025', '2026'])
    expect(geometry.columns.map((column) => column.inScope)).toEqual([false, false, true])
    expect(geometry.columns[2]).toMatchObject({
      partial: true,
      name: '2026, in +120.00, out -480.00, net -360.00',
    })
    expect(scopeFromColumns(context, 1, 1)).toEqual({ start: '2025-01-01', end: '2025-12-31' })
  })

  it('refuses a summary whose cash flow does not line up with the chart context', () => {
    const chart = megastore()
    chart.response.summary!.cashflow.net = ['-30000']
    expect(() => cashflowGeometry(chart, 200, 100)).toThrow(/2 chart-context periods/)
    const extra = megastore()
    extra.response.summary!.cashflow.labels.push('Oct 2026')
    expect(() => cashflowGeometry(extra, 200, 100)).toThrow(/labels/)
  })

  it('draws nothing for a response without a summary', () => {
    const empty = megastore()
    const response = { ...empty.response }
    delete response.summary
    expect(cashflowGeometry({ ...empty, response }, 200, 100).columns).toEqual([])
  })
})

describe('cursorGeometry', () => {
  it('draws the cursor row inside each column on the chart scale', () => {
    const chart = megastore()
    const bars = cursorGeometry(chart, { in: ['0', '6000'], out: ['-30000', '-12000'] }, 108)
    expect(bars).toEqual([
      { inTop: 24, inHeight: 0, outHeight: 50 },
      { inTop: 14, inHeight: 10, outHeight: 20 },
    ])
  })
})

describe('scopeFromColumns', () => {
  it('spans the first column start through the last column end in either order', () => {
    const context = [month(2026, 7), month(2026, 8), month(2026, 9)]
    expect(scopeFromColumns(context, 0, 2)).toEqual({ start: '2026-07-01', end: '2026-09-30' })
    expect(scopeFromColumns(context, 2, 0)).toEqual({ start: '2026-07-01', end: '2026-09-30' })
    expect(scopeFromColumns(context, 1, 1)).toEqual({ start: '2026-08-01', end: '2026-08-31' })
  })
})

describe('tickEvery', () => {
  it('labels every column when they fit and thins the labels when they do not', () => {
    expect(tickEvery(13, 650, 'month')).toBe(1)
    expect(tickEvery(30, 300, 'day')).toBe(4)
    expect(tickEvery(5, 0, 'year')).toBe(1)
  })

  it('thins month labels to a step that divides the year, so January keeps its label', () => {
    expect(tickEvery(34, 680, 'month')).toBe(2)
    expect(tickEvery(34, 300, 'month')).toBe(6)
    expect(tickEvery(60, 100, 'month')).toBe(12)
    const months = [month(2025, 11), month(2025, 12), month(2026, 1), month(2026, 2)]
    const geometry = cashflowGeometry(
      entry(months, {
        labels: ['Nov 2025', 'Dec 2025', 'Jan 2026', 'Feb 2026'],
        in: ['0', '0', '0', '0'],
        out: ['0', '0', '0', '0'],
        net: ['0', '0', '0', '0'],
      }),
      400,
      100,
    )
    const ticked = geometry.columns.filter((column) => column.tickOrder % 2 === 0)
    expect(ticked.map((column) => column.tick)).toEqual(['Nov', '2026'])
  })
})

describe('cashflowCursor', () => {
  type Row = NonNullable<ViewProjection['aggregate_rows']>[number]

  function row(identity: string, index: number, extra: Partial<Row> = {}): Row {
    return testAggregateRow({ identity, index, label: identity, ...extra })
  }

  function grouped(rows: Row[], query = 'v=1'): ViewProjection {
    const projection = testProjection({ canonical_query: query, aggregate_rows: rows })
    delete projection.detail_rows
    return projection
  }

  const series = new Map<string, InsightsSeries>([
    [
      'megastore',
      { ...testSeries('megastore'), context: { in: ['0', '12000'], out: ['-30000', '-48000'] } },
    ],
  ])
  const seriesFor = (identity: string) => series.get(identity)

  it("reads the cursor row's series in grouped views", () => {
    const projection = grouped([row('grocer', 0), row('megastore', 1, { label: 'Megastore' })])
    expect(cashflowCursor(projection, 1, megastore(), seriesFor)).toEqual({
      identity: 'megastore',
      label: 'Megastore',
      in: ['0', '12000'],
      out: ['-30000', '-48000'],
    })
    expect(cashflowCursor(projection, 0, megastore(), seriesFor)).toBeUndefined()
  })

  it('ignores series that do not belong to the drawn query or context', () => {
    const other = grouped([row('megastore', 0)], 'v=2')
    expect(cashflowCursor(other, 0, megastore(), seriesFor)).toBeUndefined()
    const short = new Map([['megastore', testSeries('megastore')]])
    const projection = grouped([row('megastore', 0)])
    expect(cashflowCursor(projection, 0, megastore(), (id) => short.get(id))).toBeUndefined()
  })

  it('reads the cursor row when the view differs from the drawn query only by sort', () => {
    const projection = grouped([row('megastore', 0)], 'sort=name%3Aasc&v=1')
    expect(cashflowCursor(projection, 0, megastore(), seriesFor)?.identity).toBe('megastore')
  })

  it("selects the cursor period's whole column in the Time grouping", () => {
    const period = { granularity: 'month', year: 2026, month: 9 }
    const projection = grouped([row('time:sep', 0, { dimension: 'time', period })])
    expect(cashflowCursor(projection, 0, megastore(), seriesFor)).toEqual({
      identity: 'time:sep',
      in: ['0', '12000'],
      out: ['0', '-48000'],
    })
  })

  it('draws no cursor in transaction views', () => {
    const projection = testProjection({ canonical_query: 'v=1' })
    expect(projection.detail_rows).toBeDefined()
    expect(cashflowCursor(projection, 0, megastore(), seriesFor)).toBeUndefined()
  })
})

describe('rangeAnchor', () => {
  /** Six columns, Jul through Dec, with `scoped` in scope. */
  const columns = (scoped: number[]) =>
    Array.from({ length: 6 }, (_, index) => ({ inScope: scoped.includes(index) }))

  it('anchors on the scope edge farther from the clicked column', () => {
    expect(rangeAnchor(columns([2]), 0, 5)).toBe(2)
    expect(rangeAnchor(columns([1, 2]), 3, 0)).toBe(1)
    expect(rangeAnchor(columns([1, 2]), 0, 5)).toBe(2)
    // A Jul–Dec scope shift-clicked on Aug becomes Aug–Dec.
    expect(rangeAnchor(columns([0, 1, 2, 3, 4, 5]), 1, 0)).toBe(5)
    expect(rangeAnchor(columns([0, 1, 2, 3, 4, 5]), 4, 0)).toBe(0)
  })

  it('anchors on the start edge when both edges are equally far', () => {
    expect(rangeAnchor(columns([1, 2, 3]), 2, 0)).toBe(1)
  })

  it('starts from the focused column when nothing is in scope', () => {
    expect(rangeAnchor(columns([]), 0, 3)).toBe(3)
  })
})

describe('rowAmounts', () => {
  it("formats the cursor row's In and Out for one column", () => {
    const cursor = { identity: 'm', label: 'Megastore', in: ['0', '12000'], out: ['0', '-48000'] }
    expect(rowAmounts(cursor, 1, 2)).toEqual({
      label: 'Megastore',
      in: '+120.00',
      out: '-480.00',
    })
    expect(rowAmounts({ identity: 'm', in: cursor.in, out: cursor.out }, 1, 2)).toBeUndefined()
    expect(rowAmounts(undefined, 1, 2)).toBeUndefined()
  })
})
