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
  /** The accessible name. */
  label: string
  /** Visible text when it differs from the accessible name. */
  text?: string
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

const timeUnits: Record<string, string> = { year: 'Years', month: 'Months', day: 'Days' }
const timeUnit: Record<string, string> = { year: 'Year', month: 'Month', day: 'Day' }

/** The root crumb names the view `view.back-to` depth 0 returns to, as the server reports it. */
function rootLabel(projection: ViewProjection): string {
  const { mode, grouping, time_granularity } = projection.view.root
  if (mode === 'detail') return 'All transactions'
  if (grouping === 'time') return timeUnits[time_granularity] ?? 'Time'
  return dimension(grouping)?.plural ?? grouping
}

/**
 * The command bar's trail: the top-level grouping, one crumb per drill frame, and the
 * breakdown when the view is sub-grouped. Every crumb but the last returns to its level.
 */
export function breadcrumbTrail(projection: ViewProjection): Crumb[] {
  const frames = drillFrames(projection)
  const labels = [rootLabel(projection), ...frames.map((frame) => frame.label)]
  const subGrouping = projection.view.sub_grouping
  if (subGrouping) labels.push(`by ${breakdownName(projection, subGrouping)}`)
  const last = labels.length - 1
  return labels.map((label, index) =>
    index === last ? { label, current: true } : { label, depth: index, current: false },
  )
}

/** A breakdown's name; a Time breakdown is named by its unit, such as Day. */
function breakdownName(projection: ViewProjection, subGrouping: string): string {
  if (subGrouping === 'time') return timeUnit[projection.view.time_granularity] ?? 'Time'
  return dimension(subGrouping)?.singular ?? subGrouping
}

/** Break down by choices inside a drill: Transactions and the dimensions not yet drilled. */
export function breakDownOptions(projection: ViewProjection): BreakDownOption[] {
  const frames = drillFrames(projection)
  if (frames.length === 0) return []
  const used = new Set(frames.map((frame) => frame.dimension))
  const remaining = dimensions.filter((entry) => !used.has(entry.value))
  const unit = timeUnit[projection.view.time_granularity]
  const timeChosen = projection.view.sub_grouping === 'time' && unit !== undefined
  return [
    { value: 'transactions', label: 'Transactions' },
    ...remaining.map((entry) =>
      entry.value === 'time' && timeChosen
        ? { value: entry.value, label: `Time, ${unit}`, text: `Time · ${unit}` }
        : { value: entry.value, label: entry.singular },
    ),
  ]
}
