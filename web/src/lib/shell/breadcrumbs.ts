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

function rootLabel(projection: ViewProjection, drilled: boolean): string {
  if (!drilled && projection.view.mode === 'detail') return 'All transactions'
  return dimension(projection.view.grouping)?.plural ?? projection.view.grouping
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
