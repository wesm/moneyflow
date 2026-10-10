import { describe, expect, it } from 'vitest'

import type { InsightsBody, InsightsResponse, InsightsSeries } from '../api/client'
import type { components } from '../api/schema'
import { InsightsCache } from '../insights/cache.svelte'
import { testInsights, testSeries } from '../../test/insights'
import { testProjection } from '../../test/projection'
import {
  rowSeries,
  sparkline,
  SPARK_HEIGHT,
  SPARK_WIDTH,
  tableInsights,
  type RowInsights,
} from './trend'

type Month = components['schemas']['InsightsPeriodRange']

/** Twelve trend months from Nov 2025 to Oct 2026; `absent` leading months, the last `partial`. */
function months(absent = 0, partial = false): Month[] {
  return Array.from({ length: 12 }, (_, index) => {
    const month = ((10 + index) % 12) + 1
    const year = index < 2 ? 2025 : 2026
    const start = `${year}-${String(month).padStart(2, '0')}-01`
    return {
      start,
      end: start,
      period: { granularity: 'month', year, month },
      absent: index < absent,
      partial: partial && index === 11,
    }
  })
}

const zero = SPARK_HEIGHT / 2

describe('sparkline', () => {
  it('spreads twelve months across the width around a zero line at mid-height', () => {
    const trend = ['0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '-400']
    const line = sparkline(trend, months())
    expect(line.zero).toBe(zero)
    expect(line.segments).toHaveLength(1)
    const points = line.segments[0]!
    expect(points).toHaveLength(12)
    expect(points[0]!.x).toBeLessThan(points[1]!.x)
    expect(points[11]!.x).toBeLessThanOrEqual(SPARK_WIDTH)
    expect(points[0]!.y).toBe(zero)
  })

  it('draws income above and spending below the zero line, scaled to the row’s largest month', () => {
    const trend = ['200', '-400', '0', '0', '0', '0', '0', '0', '0', '0', '0', '-100']
    const [points] = sparkline(trend, months()).segments
    const [income, spending, flat] = points!
    expect(income!.y).toBeLessThan(zero)
    expect(spending!.y).toBeGreaterThan(zero)
    expect(flat!.y).toBe(zero)
    // -400 reaches the edge; +200 goes half as far the other way.
    expect(spending!.y - zero).toBeCloseTo(2 * (zero - income!.y), 5)
    expect(spending!.y).toBeLessThan(SPARK_HEIGHT)
  })

  it('reads amounts beyond a JavaScript number exactly', () => {
    const huge = '-9223372036854775807'
    const trend = [huge, '-9223372036854775806', ...Array<string>(10).fill('0')]
    const [points] = sparkline(trend, months()).segments
    expect(points![0]!.y).toBeGreaterThanOrEqual(points![1]!.y)
    expect(points![1]!.y).toBeGreaterThan(zero)
  })

  it('leaves absent months out as a gap rather than drawing zero', () => {
    const trend = ['0', '0', '0', '-100', '-200', '0', '0', '0', '0', '0', '0', '-50']
    const line = sparkline(trend, months(3))
    expect(line.segments).toHaveLength(1)
    expect(line.segments[0]).toHaveLength(9)
    const first = sparkline(trend, months()).segments[0]![3]!
    expect(line.segments[0]![0]).toEqual(first)
  })

  it('draws a flat line at zero for a row with no money in any month', () => {
    const line = sparkline(Array<string>(12).fill('0'), months())
    expect(line.segments[0]!.every((point) => point.y === zero)).toBe(true)
  })

  it('marks the last month hollow only when it is partial', () => {
    const trend = [...Array<string>(11).fill('-100'), '-20']
    expect(sparkline(trend, months()).end).toMatchObject({ hollow: false })
    const partial = sparkline(trend, months(0, true))
    expect(partial.end).toMatchObject({ hollow: true })
    expect(partial.end).toMatchObject(partial.segments[0]![11]!)
  })

  it('draws nothing when every month is absent', () => {
    const line = sparkline(Array<string>(12).fill('0'), months(12))
    expect(line.segments).toEqual([])
    expect(line.end).toBeUndefined()
  })
})

describe('rowSeries', () => {
  const response = testInsights()
  const series = testSeries('merchant:a')
  const insights = (
    loaded: InsightsResponse | undefined,
    found: InsightsSeries | undefined,
    failed = false,
  ): RowInsights => ({ response: loaded, failed, seriesFor: () => found })

  it('pairs a loaded series with the response it is read against', () => {
    expect(rowSeries(insights(response, series), 'merchant:a')).toEqual({
      state: 'ready',
      series,
      response,
    })
  })

  it('is loading while the series or the displayed query’s ranges are missing', () => {
    expect(rowSeries(insights(response, undefined), 'merchant:a')).toEqual({ state: 'loading' })
    expect(rowSeries(insights(undefined, series), 'merchant:a')).toEqual({ state: 'loading' })
  })

  it('stops loading once a series request failed', () => {
    expect(rowSeries(insights(response, undefined, true), 'merchant:a')).toEqual({
      state: 'failed',
    })
    expect(rowSeries(insights(response, series, true), 'merchant:a').state).toBe('ready')
  })
})

describe('tableInsights', () => {
  function view(query: string) {
    const base = testProjection()
    return testProjection({
      canonical_query: query,
      view: { ...base.view, currency: { currency: 'USD', scale: 2 } },
    })
  }

  it('reads the cache’s entry only while it belongs to the displayed query', async () => {
    const answers: { resolve: (response: InsightsResponse) => void; reject: () => void }[] = []
    const bodies: InsightsBody[] = []
    const cache = new InsightsCache(
      (body) =>
        new Promise((resolve, reject) => {
          bodies.push(body)
          answers.push({ resolve, reject })
        }),
      () => undefined,
    )
    cache.sync(view('q=aug'), ['merchant:a'])
    answers[0]!.resolve(testInsights({ query: 'q=aug', series: [testSeries('merchant:a')] }))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(tableInsights(cache, 'q=aug').response?.query).toBe('q=aug')
    expect(tableInsights(cache, 'q=aug').seriesFor('merchant:a')?.identity).toBe('merchant:a')

    // The previous month's entry stays on screen for the charts, but not for this table.
    cache.sync(view('q=sep'), ['merchant:a'])
    const september = tableInsights(cache, 'q=sep')
    expect(september.response).toBeUndefined()
    expect(rowSeries(september, 'merchant:a')).toEqual({ state: 'loading' })

    answers[1]!.reject()
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(rowSeries(tableInsights(cache, 'q=sep'), 'merchant:a')).toEqual({ state: 'failed' })
    expect(bodies.map((body) => body.query)).toEqual(['q=aug', 'q=sep'])
  })
})
