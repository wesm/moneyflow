import type { InsightsResponse, ViewProjection } from '../api/client'
import { formatMinor, tenthsPercent, unsignedZero } from '../money/format'
import { periodName, samePeriod } from './cashflow'
import { ratioBasisPoints } from './scale'

type Summary = NonNullable<InsightsResponse['summary']>
type ContextPeriod = InsightsResponse['ranges']['context'][number]

/** Spending and Income rankings, each the top seven plus an others entry. */
export type Composition = NonNullable<Summary['composition']>
export type CompositionSide = keyof Composition

/** One ranked entry of a composition, ready to draw. */
export interface CompositionBar {
  /** Unique in its list: the row identity, the group key, or `others`. */
  key: string
  /** The table row this bar stands for; absent for others and for category groups. */
  identity?: string
  label: string
  /** The net share, such as `6.2%`; it equals the table's Share column. */
  share: string
  /** Net as the table shows it, so a zero reads `0.00`; the name keeps the signed amount. */
  net: string
  /** The bar's length in basis points of the longest bar's. */
  length: number
  /** Folds the rows below the top seven, so it stands for no single row. */
  others: boolean
  /** The accessible name: `{label}, {share}, {net}`, such as `Rent, 40.0%, -2,000.00`. */
  name: string
}

/** The bars for one side of `composition`, sized by net share against the largest one. */
export function compositionBars(
  composition: Composition,
  side: CompositionSide,
  scale: number,
): CompositionBar[] {
  const entries = composition[side]
  const longest = String(Math.max(0, ...entries.map((entry) => entry.share_tenths)))
  return entries.map((entry) => {
    const others = entry.others !== undefined && entry.others > 0
    const share = tenthsPercent(entry.share_tenths)
    const net = formatMinor(entry.net, scale)
    return {
      key: others ? 'others' : (entry.identity ?? entry.key ?? entry.label),
      ...(others || entry.identity === undefined ? {} : { identity: entry.identity }),
      label: entry.label,
      share,
      net: unsignedZero(net),
      length: ratioBasisPoints(String(entry.share_tenths), longest),
      others,
      name: `${entry.label}, ${share}, ${net}`,
    }
  })
}

/** What a composition click needs from the view controller. */
export interface RowMover {
  readonly neighbors: readonly ViewProjection[]
  moveCursorTo(index: number): Promise<void>
  showAnchor(identity: string): Promise<void>
}

/**
 * Moves the cursor to the aggregate row `identity`: at once when the loaded window or a cached
 * neighbor holds it, otherwise by loading the window that holds it.
 */
export function moveToRow(
  controller: RowMover,
  projection: ViewProjection,
  identity: string,
): Promise<void> {
  for (const window of [projection, ...controller.neighbors]) {
    const row = window.aggregate_rows?.find((candidate) => candidate.identity === identity)
    if (row !== undefined) return controller.moveCursorTo(row.index)
  }
  return controller.showAnchor(identity)
}

/** The chart-context index of a Time row's period, which `period_composition` aligns with. */
export function periodIndex(
  context: readonly ContextPeriod[],
  period: ContextPeriod['period'] | undefined,
): number | undefined {
  if (period === undefined) return undefined
  const index = context.findIndex((candidate) => samePeriod(candidate.period, period))
  return index < 0 ? undefined : index
}

/** The Time grouping's panel: one period's category-group composition and its name. */
export interface PeriodShare {
  label: string
  composition: Composition
}

/**
 * The composition of the cursor's period, or of the last chart-context period when the cursor
 * names none of them. Reads only the summary, so moving the cursor never waits. Throws unless
 * `period_composition` holds one composition per chart-context period.
 */
export function periodShare(
  response: InsightsResponse,
  period: ContextPeriod['period'] | undefined,
): PeriodShare | undefined {
  const periods = response.summary?.period_composition
  const context = response.ranges.context
  if (periods === undefined || context.length === 0) return undefined
  if (periods.length !== context.length) {
    throw new Error(
      `Period composition has ${periods.length} compositions for ${context.length} periods.`,
    )
  }
  const index = periodIndex(context, period) ?? context.length - 1
  const label = response.summary?.cashflow.labels[index] ?? ''
  return { label: periodName(context[index]!, label), composition: periods[index]! }
}
