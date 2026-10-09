import type { ViewProjection } from '../api/client'
import { drillFrames } from './drills'

export interface Crumb {
  label: string
  /** Drill frames `view.back-to` keeps to return here; undefined for the current crumb. */
  depth?: number
  current: boolean
}

export interface BreakDownOption {
  /** A dimension, or `transactions` for the drilled transactions themselves. */
  value: string
  label: string
}

const dimensions = [
  { value: 'merchant', singular: 'Merchant', plural: 'Merchants' },
  { value: 'category', singular: 'Category', plural: 'Categories' },
  { value: 'group', singular: 'Group', plural: 'Groups' },
  { value: 'account', singular: 'Account', plural: 'Accounts' },
  { value: 'time', singular: 'Time', plural: 'Time' },
] as const

function dimension(value: string) {
  return dimensions.find((entry) => entry.value === value)
}

interface RootView {
  mode: string
  grouping: string
  /** The time unit, when the view names it; a currency-scoped return frame leaves it out. */
  granularity?: string
}

/**
 * The view the root crumb returns to. `view.back-to` depth 0 steps back through the saved
 * `return` frames (newest last) to the newest one without a drill. A drill opened from a link
 * has none; it is cut back to the top level of its own grouping.
 */
function rootView(projection: ViewProjection, drilled: boolean): RootView {
  const { mode, grouping } = projection.view
  if (!drilled) return { mode, grouping, granularity: projection.view.time_granularity }
  const returns = new URLSearchParams(projection.canonical_query)
    .getAll('return')
    .map((frame) => new URLSearchParams(frame.slice(frame.indexOf(':') + 1)))
  const root = returns.findLast((frame) => !frame.has('drill'))
  if (!root) return { mode: 'aggregate', grouping }
  return {
    mode: root.get('mode') ?? 'aggregate',
    grouping: root.get('group') ?? 'merchant',
    ...(root.has('cur') ? {} : { granularity: root.get('time') ?? 'year' }),
  }
}

const timeUnits: Record<string, string> = { year: 'Years', month: 'Months', day: 'Days' }

function rootLabel(projection: ViewProjection, drilled: boolean): string {
  const root = rootView(projection, drilled)
  if (root.mode === 'detail') return 'All transactions'
  if (root.grouping === 'time') return timeUnits[root.granularity ?? ''] ?? 'Time'
  return dimension(root.grouping)?.plural ?? root.grouping
}

/**
 * The command bar's trail: the top-level grouping, one crumb per drill frame, and the
 * breakdown when the view is sub-grouped. Every crumb but the last returns to its level.
 */
export function breadcrumbTrail(projection: ViewProjection): Crumb[] {
  const frames = drillFrames(projection)
  const labels = [rootLabel(projection, frames.length > 0), ...frames.map((frame) => frame.label)]
  const subGrouping = projection.view.sub_grouping
  if (subGrouping) labels.push(`by ${dimension(subGrouping)?.singular ?? subGrouping}`)
  const last = labels.length - 1
  return labels.map((label, index) =>
    index === last ? { label, current: true } : { label, depth: index, current: false },
  )
}

/** Break down by choices inside a drill: Transactions and the dimensions not yet drilled. */
export function breakDownOptions(projection: ViewProjection): BreakDownOption[] {
  const frames = drillFrames(projection)
  if (frames.length === 0) return []
  const used = new Set(frames.map((frame) => frame.dimension))
  const remaining = dimensions.filter((entry) => !used.has(entry.value))
  return [
    { value: 'transactions', label: 'Transactions' },
    ...remaining.map((entry) => ({ value: entry.value, label: entry.singular })),
  ]
}
