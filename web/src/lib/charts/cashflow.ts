import type { InsightsResponse, InsightsSeries, ViewProjection } from '../api/client'
import type { InsightsEntry } from '../insights/cache.svelte'
import { formatMinor, parseMinor } from '../money/format'
import { dayLabel, MONTHS, type Period } from '../shell/period'
import { maxMagnitude, ratioBasisPoints } from './scale'

type ContextPeriod = InsightsResponse['ranges']['context'][number]
type Cashflow = NonNullable<InsightsResponse['summary']>['cashflow']

/** Room above and below the plot, so the Net line's dots are never clipped. */
const inset = 4
const basisPoints = 10000
/** The narrowest room an axis label needs, in pixels. */
const tickRoom = 40

/** One chart-context period: its slot, its bars, and its Net dot, in pixels. */
export interface ColumnGeometry {
  /** The slot's left edge and width; slots split the chart's width evenly. */
  x: number
  width: number
  /** The In bar rises from zero to `inTop`. */
  inTop: number
  inHeight: number
  /** The Out bar falls from zero. */
  outHeight: number
  netY: number
  partial: boolean
  /** The period meets the view's scope, so the scope band covers it. */
  inScope: boolean
  /** The accessible name: `{label}, in {in}, out {out}, net {net}`. */
  name: string
  /** The period's name, such as `Sep 2026` or `Sep 15, 2026`. */
  label: string
  /** The axis label: the day number, the month's short name, or the year. */
  tick: string
  /** Shows `tick` when divisible by `tickEvery`'s step; months count from year zero. */
  tickOrder: number
  in: string
  out: string
  net: string
}

export interface CashflowGeometry {
  columns: ColumnGeometry[]
  zeroY: number
  /** The Net line through each column's center. */
  netPath: string
}

/** The cursor's In and Out per chart-context period. */
export interface CashflowCursor {
  identity: string
  /** The cursor row's name; absent when the cursor is a Time period drawn as its own column. */
  label?: string
  in: readonly string[]
  out: readonly string[]
}

export interface CursorBar {
  inTop: number
  inHeight: number
  outHeight: number
}

interface Scale {
  /** Max In plus max |Out|: one magnitude scale for both directions. */
  span: string
  plot: number
  zeroY: number
}

const emptyCashflow: Cashflow = { labels: [], in: [], out: [], net: [], count: [] }

function cashflowOf(entry: InsightsEntry): Cashflow {
  return entry.response.summary?.cashflow ?? emptyCashflow
}

function scaleOf(cashflow: Cashflow, height: number): Scale {
  const rising = maxMagnitude(cashflow.in)
  const span = (parseMinor(rising) + parseMinor(maxMagnitude(cashflow.out))).toString()
  const plot = Math.max(height - 2 * inset, 0)
  const share = span === '0' ? basisPoints / 2 : ratioBasisPoints(rising, span)
  return { span, plot, zeroY: inset + (plot * share) / basisPoints }
}

/** The pixel length of `value` on the scale; money that rounds to nothing keeps one pixel. */
function extent(value: string, scale: Scale): number {
  if (parseMinor(value) === 0n) return 0
  return Math.max((scale.plot * ratioBasisPoints(value, scale.span)) / basisPoints, 1)
}

/** A period's name: the day as `Sep 15, 2026`, or the server's label for a month or year. */
export function periodName(period: ContextPeriod, label: string): string {
  return period.period.granularity === 'day' ? dayLabel(period.start) : label
}

function tickOf(period: ContextPeriod, label: string): string {
  const { granularity, year, month, day } = period.period
  if (granularity === 'day') return String(day ?? '')
  if (granularity === 'month' && month !== undefined) {
    return month === 1 ? String(year) : (MONTHS[month - 1] ?? label)
  }
  return label
}

function tickOrderOf(period: ContextPeriod, index: number): number {
  const { granularity, year, month } = period.period
  if (granularity === 'month' && month !== undefined) return year * 12 + month - 1
  return granularity === 'year' ? year : index
}

function meetsScope(period: ContextPeriod, ranges: InsightsResponse['ranges']): boolean {
  const scope = ranges.scope
  if (ranges.kind === 'all' || scope === undefined) return false
  return period.start <= scope.end && period.end >= scope.start
}

/** Throws unless every cash-flow array has one value per chart-context period. */
function checkAligned(cashflow: Cashflow, periods: number): void {
  for (const field of ['labels', 'in', 'out', 'net'] as const) {
    if (cashflow[field].length !== periods) {
      throw new Error(
        `Cash flow ${field} has ${cashflow[field].length} values for ` +
          `${periods} chart-context periods.`,
      )
    }
  }
}

/** The value at `index`, which `checkAligned` has already proved present. */
function valueAt(values: readonly string[], index: number): string {
  const value = values[index]
  if (value === undefined) throw new Error(`Cash flow has no value at period ${index}.`)
  return value
}

interface Layout {
  cashflow: Cashflow
  ranges: InsightsResponse['ranges']
  digits: number
  scale: Scale
  slot: number
}

function columnAt(layout: Layout, period: ContextPeriod, index: number): ColumnGeometry {
  const { cashflow, ranges, digits, scale, slot } = layout
  const rising = valueAt(cashflow.in, index)
  const falling = valueAt(cashflow.out, index)
  const net = valueAt(cashflow.net, index)
  const server = valueAt(cashflow.labels, index)
  const label = periodName(period, server)
  const netExtent = extent(net, scale)
  const inHeight = extent(rising, scale)
  const amounts = {
    in: formatMinor(rising, digits),
    out: formatMinor(falling, digits),
    net: formatMinor(net, digits),
  }
  return {
    x: index * slot,
    width: slot,
    inTop: scale.zeroY - inHeight,
    inHeight,
    outHeight: extent(falling, scale),
    netY: parseMinor(net) < 0n ? scale.zeroY + netExtent : scale.zeroY - netExtent,
    partial: period.partial,
    inScope: meetsScope(period, ranges),
    name: `${label}, in ${amounts.in}, out ${amounts.out}, net ${amounts.net}`,
    label,
    tick: tickOf(period, server),
    tickOrder: tickOrderOf(period, index),
    ...amounts,
  }
}

/**
 * Lays out the cash-flow chart for `entry` in a `width` × `height` box: one column per
 * chart-context period, In rising and Out falling from one zero on one magnitude scale.
 * Throws when the summary's cash flow does not line up with the chart context.
 */
export function cashflowGeometry(
  entry: InsightsEntry,
  width: number,
  height: number,
): CashflowGeometry {
  const { ranges, scale: digits, summary } = entry.response
  if (summary === undefined) return { columns: [], zeroY: 0, netPath: '' }
  const cashflow = summary.cashflow
  checkAligned(cashflow, ranges.context.length)
  const count = ranges.context.length
  const scale = scaleOf(cashflow, height)
  const layout = { cashflow, ranges, digits, scale, slot: count === 0 ? 0 : width / count }
  const columns = ranges.context.map((period, index) => columnAt(layout, period, index))
  const points = columns.map((column) => `${column.x + column.width / 2},${column.netY}`)
  return { columns, zeroY: scale.zeroY, netPath: points.length ? `M${points.join('L')}` : '' }
}

/** The cursor's bars inside each column, on the chart's scale for the same `height`. */
export function cursorGeometry(
  entry: InsightsEntry,
  cursor: Pick<CashflowCursor, 'in' | 'out'>,
  height: number,
): CursorBar[] {
  const scale = scaleOf(cashflowOf(entry), height)
  return cursor.in.map((rising, index) => {
    const inHeight = extent(rising, scale)
    return {
      inTop: scale.zeroY - inHeight,
      inHeight,
      outHeight: extent(cursor.out[index] ?? '0', scale),
    }
  })
}

/** The range from one column through another, in either order: a click or a drag's scope. */
export function scopeFromColumns(
  context: readonly ContextPeriod[],
  first: number,
  last: number,
): Period {
  const start = context[Math.min(first, last)]
  const end = context[Math.max(first, last)]
  if (start === undefined || end === undefined) {
    throw new Error(`Columns ${first} and ${last} are outside the ${context.length}-period chart.`)
  }
  return { start: start.start, end: end.end }
}

/** Steps that divide a year, so thinned month labels always include January. */
const monthSteps = [1, 2, 3, 4, 6, 12]

/**
 * The step between labeled columns, so each label keeps its room in `width` pixels. Month
 * steps divide twelve, and a column is labeled when its `tickOrder` is a multiple of the step.
 */
export function tickEvery(
  count: number,
  width: number,
  granularity: InsightsResponse['ranges']['context_granularity'],
): number {
  if (count === 0 || width <= 0) return 1
  const step = Math.max(1, Math.ceil((count * tickRoom) / width))
  if (granularity !== 'month') return step
  return monthSteps.find((candidate) => candidate >= step) ?? 12
}

/** Whether two periods are the same day, month, or year. */
export function samePeriod(left: ContextPeriod['period'], right: ContextPeriod['period']): boolean {
  return (
    left.granularity === right.granularity &&
    left.year === right.year &&
    left.month === right.month &&
    left.day === right.day
  )
}

/**
 * What the chart draws solid for the cursor row: its own series in grouped views, or the
 * whole column of the cursor's period in the Time grouping. Transaction views have none.
 * Only series for the drawn query count, so a stale chart never shows another view's row.
 */
export function cashflowCursor(
  projection: ViewProjection,
  cursorIndex: number,
  entry: InsightsEntry,
  seriesFor: (identity: string) => InsightsSeries | undefined,
): CashflowCursor | undefined {
  if (entry.query !== projection.canonical_query || projection.detail_rows) return undefined
  const row = projection.aggregate_rows?.find((candidate) => candidate.index === cursorIndex)
  if (row === undefined) return undefined
  const context = entry.response.ranges.context
  if (row.period !== undefined) {
    const period = row.period
    const selected = context.findIndex((candidate) => samePeriod(candidate.period, period))
    if (selected < 0) return undefined
    const cashflow = cashflowOf(entry)
    const only = (values: string[]) => values.map((value, i) => (i === selected ? value : '0'))
    return { identity: row.identity, in: only(cashflow.in), out: only(cashflow.out) }
  }
  const series = seriesFor(row.identity)
  if (series === undefined || series.context.in.length !== context.length) return undefined
  return { identity: row.identity, label: row.label, ...series.context }
}

/**
 * Where a shift-click's range starts: the scope edge farther from the clicked column, or the
 * start edge when both are equally far; the focused column when no column is in scope.
 */
export function rangeAnchor(
  columns: readonly Pick<ColumnGeometry, 'inScope'>[],
  clicked: number,
  focused: number,
): number {
  const first = columns.findIndex((column) => column.inScope)
  if (first < 0) return focused
  const last = columns.findLastIndex((column) => column.inScope)
  return Math.abs(clicked - first) >= Math.abs(clicked - last) ? first : last
}

/** The cursor row's amounts in one column, for the tooltip; none for a Time period. */
export interface RowAmounts {
  label: string
  in: string
  out: string
}

export function rowAmounts(
  cursor: CashflowCursor | undefined,
  index: number,
  scale: number,
): RowAmounts | undefined {
  if (cursor?.label === undefined) return undefined
  return {
    label: cursor.label,
    in: formatMinor(cursor.in[index] ?? '0', scale),
    out: formatMinor(cursor.out[index] ?? '0', scale),
  }
}

/** Shows a zero amount as a dash, as the table does; the accessible names keep `+0.00`. */
export function dashZero(display: string): string {
  return /^\+0(\.0+)?$/.test(display) ? '—' : display
}
