import type { InsightsResponse, InsightsSeries } from '../api/client'
import type { components } from '../api/schema'
import { maxMagnitude, ratioBasisPoints } from '../charts/scale'
import type { InsightsCache } from '../insights/cache.svelte'
import { insightsQuery } from '../insights/requests'
import { MONTHS, partialName } from '../shell/period'

type TrendMonth = components['schemas']['InsightsPeriodRange']

/** The sparkline's SVG coordinate size. */
export const SPARK_WIDTH = 56
export const SPARK_HEIGHT = 24
/** Room at every edge for the last month's dot. */
const INSET = 2.5
/** The radius of the last month's dot. */
export const END_RADIUS = 2

export interface SparkPoint {
  readonly x: number
  readonly y: number
}

export interface Sparkline {
  /** The zero line's height; mid-height in every row, so rows compare at a glance. */
  readonly zero: number
  /** Runs of consecutive months; an absent month ends a run, leaving a gap. */
  readonly segments: readonly (readonly SparkPoint[])[]
  /** The last month's dot, even for a month in progress; none when the month is absent. */
  readonly end?: SparkPoint
  /** The last month's name when it is partial, such as `Oct 2026 (to date)`. */
  readonly partial?: string
}

function partialMonth(month: TrendMonth | undefined, today: string): string | undefined {
  if (!month?.partial) return undefined
  const { year, month: number } = month.period
  const name = number === undefined ? String(year) : `${MONTHS[number - 1] ?? ''} ${year}`
  return partialName(name, month, today)
}

function rounded(value: number): number {
  return Math.round(value * 100) / 100
}

/**
 * The 12-month Net sparkline of a row: `trend[i]` is the Net of `months[i]`, a minor-unit
 * string; `today` is the server's date. Income rises above the zero line and spending falls
 * below it, scaled to the row's largest month with exact integer ratios.
 */
export function sparkline(
  trend: readonly string[],
  months: readonly TrendMonth[],
  today: string,
): Sparkline {
  const zero = SPARK_HEIGHT / 2
  const reach = zero - INSET
  const step = months.length > 1 ? (SPARK_WIDTH - 2 * INSET) / (months.length - 1) : 0
  const present = (index: number) => months[index]?.absent === false && trend[index] !== undefined
  const largest = maxMagnitude(trend.filter((_, index) => present(index)))
  const segments: SparkPoint[][] = []
  let run: SparkPoint[] | undefined
  for (let index = 0; index < months.length; index += 1) {
    const net = trend[index]
    if (!present(index) || net === undefined) {
      run = undefined
      continue
    }
    const offset = (reach * ratioBasisPoints(net, largest)) / 10000
    const point = {
      x: rounded(INSET + step * index),
      y: rounded(net.startsWith('-') ? zero + offset : zero - offset),
    }
    if (run === undefined) {
      run = []
      segments.push(run)
    }
    run.push(point)
  }
  const final = present(months.length - 1) ? segments.at(-1)?.at(-1) : undefined
  if (final === undefined) return { zero, segments }
  const partial = partialMonth(months.at(-1), today)
  return partial === undefined
    ? { zero, segments, end: final }
    : { zero, segments, end: final, partial }
}

/** What the grouped table reads from the displayed view's insights. */
export interface RowInsights {
  /** The displayed query's insights, for its ranges and scale; none until they arrive. */
  readonly response: InsightsResponse | undefined
  /** A series request failed, so missing series stop showing as loading. */
  readonly failed: boolean
  readonly seriesFor: (identity: string) => InsightsSeries | undefined
}

/**
 * The table's view of the cache. The charts keep the previous entry on screen while the next
 * loads; the table reads an entry's ranges only when it belongs to the displayed `query`, sort
 * aside.
 */
export function tableInsights(cache: InsightsCache, query: string): RowInsights {
  const entry = cache.entry
  return {
    response: entry?.query === insightsQuery(query) ? entry.response : undefined,
    failed: cache.status === 'failed' || cache.seriesFailed,
    seriesFor: (identity) => cache.seriesFor(identity),
  }
}

/** A row's series with the response it is read against, or why the cells cannot draw yet. */
export type RowSeries =
  | {
      readonly state: 'ready'
      readonly series: InsightsSeries
      readonly response: InsightsResponse
    }
  | { readonly state: 'loading' | 'failed' }

export function rowSeries(insights: RowInsights, identity: string): RowSeries {
  const series = insights.seriesFor(identity)
  const response = insights.response
  if (series && response) return { state: 'ready', series, response }
  return { state: insights.failed ? 'failed' : 'loading' }
}
