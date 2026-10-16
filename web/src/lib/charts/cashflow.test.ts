import { describe, expect, it } from 'vitest'

import type { InsightsResponse, InsightsSeries, ViewProjection } from '../api/client'
import type { InsightsEntry } from '../insights/cache.svelte'
import { testInsights, testSeries } from '../../test/insights'
import { testAggregateRow, testProjection } from '../../test/projection'
import {
  axisLabels,
  axisTicks,
  cashflowCursor,
  cashflowGeometry,
  cursorGeometry,
  rangeAnchor,
  rowAmounts,
  scopeFromColumns,
  type AxisLabel,
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

  it('bands only the columns that meet the scope and runs Net through a partial period', () => {
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

    // The Net line runs to the partial period's dot as to any other.
    expect(custom.netPath).toBe('M50,50L150,50L250,50')
    // Only the partial period says it runs to date; its axis label stays short.
    expect(custom.columns.map((column) => column.partial)).toEqual([false, false, true])
    expect(custom.columns[1]).toMatchObject({ label: 'Sep 2026', tick: 'Sep' })
    expect(custom.columns[2]).toMatchObject({ label: 'Oct 2026 (to date)', tick: 'Oct' })
    expect(custom.columns[2]!.name).toMatch(/^Oct 2026 \(to date\), in /)

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
    // The year in progress says so in its name, not in its axis label.
    expect(geometry.columns[2]?.name).toBe('2026 (to date), in +120.00, out -480.00, net -360.00')
    expect(scopeFromColumns(context, 1, 1)).toEqual({ start: '2025-01-01', end: '2025-12-31' })
  })

  it('names the dates a historical custom range covers in its clipped edge periods', () => {
    // Jan 15 – May 10, 2026, long before today (Oct 7): its edges are partial but not in progress.
    const clip = (period: Context[number], start: string, end: string) => ({
      ...period,
      start,
      end,
      partial: true,
    })
    const context = [
      clip(month(2026, 1), '2026-01-15', '2026-01-31'),
      month(2026, 2),
      month(2026, 3),
      month(2026, 4),
      clip(month(2026, 5), '2026-05-01', '2026-05-10'),
    ]
    const zeros = ['0', '0', '0', '0', '0']
    const flow = { labels: ['Jan 2026', 'Feb 2026', 'Mar 2026', 'Apr 2026', 'May 2026'] }
    const geometry = cashflowGeometry(
      entry(
        context,
        { ...flow, in: zeros, out: zeros, net: zeros },
        { kind: 'custom', scope: { start: '2026-01-15', end: '2026-05-10' } },
      ),
      500,
      100,
    )
    expect(geometry.columns.map((column) => column.label)).toEqual([
      'Jan 2026 (Jan 15–31)',
      'Feb 2026',
      'Mar 2026',
      'Apr 2026',
      'May 2026 (May 1–10)',
    ])
    // Both edges still draw lighter.
    expect(geometry.columns.map((column) => column.partial)).toEqual([
      true,
      false,
      false,
      false,
      true,
    ])
    expect(geometry.columns[0]!.name).toMatch(/^Jan 2026 \(Jan 15–31\), in /)

    // A clipped year names its months too.
    const years = cashflowGeometry(
      entry(
        [clip(year(2021), '2021-03-05', '2021-12-31'), year(2022)],
        { labels: ['2021', '2022'], in: ['0', '0'], out: ['0', '0'], net: ['0', '0'] },
        { kind: 'custom', scope: { start: '2021-03-05', end: '2022-12-31' } },
      ),
      200,
      100,
    )
    expect(years.columns.map((column) => column.label)).toEqual(['2021 (Mar 5 – Dec 31)', '2022'])
  })

  it('names a clipped period that holds today from its first day to date', () => {
    // Oct 5, 2026 – Jan 10, 2027 with today Oct 7: October is clipped and in progress.
    const context = [
      { ...month(2026, 10), start: '2026-10-05', partial: true },
      month(2026, 11),
      month(2026, 12),
      { ...month(2027, 1), end: '2027-01-10', partial: true },
    ]
    const zeros = ['0', '0', '0', '0']
    const geometry = cashflowGeometry(
      entry(
        context,
        {
          labels: ['Oct 2026', 'Nov 2026', 'Dec 2026', 'Jan 2027'],
          in: zeros,
          out: zeros,
          net: zeros,
        },
        { kind: 'custom', scope: { start: '2026-10-05', end: '2027-01-10' } },
      ),
      400,
      100,
    )
    expect(geometry.columns.map((column) => column.label)).toEqual([
      'Oct 2026 (Oct 5 – to date)',
      'Nov 2026',
      'Dec 2026',
      'Jan 2027 (Jan 1–10)',
    ])
  })

  it('refuses a summary whose cash flow does not line up with the chart context', () => {
    const chart = megastore()
    chart.response.summary!.cashflow.net = ['-30000']
    expect(() => cashflowGeometry(chart, 200, 100)).toThrow(/2 chart-context periods/)
    const extra = megastore()
    extra.response.summary!.cashflow.labels.push('Oct 2026')
    expect(() => cashflowGeometry(extra, 200, 100)).toThrow(/labels/)
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

describe('axisLabels', () => {
  /** Measures a label as six pixels per character. */
  const measure = (text: string) => 6 * text.length

  function chart(context: Context, width: number, granularity: Ranges['context_granularity']) {
    const zero = context.map(() => '0')
    const flow = { labels: context.map((p) => p.start), in: zero, out: zero, net: zero }
    const drawn = entry(context, flow, { context_granularity: granularity })
    const { columns } = cashflowGeometry(drawn, width, 100)
    return { columns, labels: axisLabels(columns, drawn.response.ranges, width, measure) }
  }

  function days(from: Context[number], count: number): Context {
    const first = Date.parse(`${from.start}T00:00:00Z`)
    return Array.from({ length: count }, (_, index) => {
      const date = new Date(first + index * 86_400_000)
      return day(date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate())
    })
  }

  function texts(labels: AxisLabel[]): string[] {
    return labels.map((label) => label.text)
  }

  it('labels every column when the measured labels fit', () => {
    const { labels } = chart(days(day(2026, 1, 10), 11), 440, 'day')
    expect(texts(labels)).toEqual([
      'Jan 10',
      '11',
      '12',
      '13',
      '14',
      '15',
      '16',
      '17',
      '18',
      '19',
      '20',
    ])
  })

  it('thins Day labels so a year label wider than its column keeps clear of its neighbors', () => {
    // 42 px columns, but "Dec 31" and "Jan 2026" are 36 and 48 px wide side by side.
    const { labels } = chart([day(2025, 12, 31), ...days(day(2026, 1, 1), 12)], 13 * 42, 'day')
    expect(texts(labels)).toEqual([
      '',
      'Jan 2026',
      '',
      '3',
      '',
      '5',
      '',
      '7',
      '',
      '9',
      '',
      '11',
      '',
    ])
  })

  it('moves a label at either edge inside the chart instead of cutting it off', () => {
    const first = chart(days(day(2026, 1, 1), 20), 400, 'day').labels[0]
    expect(first).toEqual({ text: 'Jan 1', offset: 0 })
    const { labels } = chart([day(2025, 12, 30), day(2025, 12, 31), day(2026, 1, 1)], 120, 'day')
    // Centered, "Jan 2026" would run from 76 to 124 px; it ends at the chart's 120 px edge.
    expect(labels[2]).toEqual({ text: 'Jan 2026', offset: -8 })
  })

  it('keeps the year label and every label apart and inside the chart at any width', () => {
    for (let length = 2; length <= 70; length += 3) {
      const context = days(day(2025, 12, 32 - Math.min(length - 1, 31)), length)
      for (let width = 60; width <= 1600; width += 23) {
        const { columns, labels } = chart(context, width, 'day')
        const shown = labels.flatMap((label, index) => {
          if (label.text === '') return []
          const left = (columns[index]?.x ?? NaN) + label.offset
          return [{ text: label.text, left, right: left + measure(label.text) }]
        })
        const where = `${length} days at ${width} px`
        expect(
          shown.map((label) => label.text),
          where,
        ).toContain('Jan 2026')
        for (const [index, label] of shown.entries()) {
          expect(label.left, where).toBeGreaterThanOrEqual(0)
          expect(label.right, where).toBeLessThanOrEqual(width)
          const next = shown[index + 1]
          if (next) expect(next.left - label.right, where).toBeGreaterThanOrEqual(6)
        }
      }
    }
  })

  it('thins month labels to a step that divides the year, so January keeps its year', () => {
    const months = Array.from({ length: 34 }, (_, index) =>
      month(2025 + Math.floor((10 + index) / 12), ((10 + index) % 12) + 1),
    )
    expect(texts(chart(months.slice(0, 13), 13 * 40, 'month').labels)).toEqual([
      'Nov',
      'Dec',
      '2026',
      'Feb',
      'Mar',
      'Apr',
      'May',
      'Jun',
      'Jul',
      'Aug',
      'Sep',
      'Oct',
      'Nov',
    ])
    const thinned = texts(chart(months, 300, 'month').labels).filter((text) => text !== '')
    expect(thinned).toEqual(['2026', 'May', 'Sep', '2027', 'May', 'Sep', '2028', 'May'])
  })

  it('draws no labels before the chart has a width', () => {
    expect(texts(chart(days(day(2026, 1, 1), 3), 0, 'day').labels)).toEqual(['', '', ''])
  })
})

describe('axisTicks', () => {
  function flat(context: Context): InsightsEntry {
    const zero = context.map(() => '0')
    return entry(context, { labels: context.map((p) => p.start), in: zero, out: zero, net: zero })
  }

  it('names the month where it changes and the year where that changes', () => {
    const context = [
      day(2024, 12, 30),
      day(2024, 12, 31),
      day(2025, 1, 1),
      day(2025, 1, 10),
      day(2025, 1, 21),
      day(2025, 2, 2),
    ]
    const { columns } = cashflowGeometry(flat(context), 400, 100)
    expect(axisTicks(columns, context, 1)).toEqual([
      'Dec 30',
      '31',
      'Jan 2025',
      '10',
      '21',
      'Feb 2',
    ])
  })

  it('names a month that changes between thinned labels and keeps the hidden ones empty', () => {
    const context = [day(2026, 1, 28), day(2026, 1, 30), day(2026, 2, 3), day(2026, 2, 9)]
    const { columns } = cashflowGeometry(flat(context), 400, 100)
    expect(axisTicks(columns, context, 2)).toEqual(['Jan 28', '', 'Feb 3', ''])
  })

  it('leaves month and year labels as they are', () => {
    const context = [month(2025, 12), month(2026, 1), month(2026, 2)]
    const { columns } = cashflowGeometry(flat(context), 400, 100)
    expect(axisTicks(columns, context, 1)).toEqual(['Dec', '2026', 'Feb'])
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
