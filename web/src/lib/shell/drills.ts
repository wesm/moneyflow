import type { ViewProjection } from '../api/client'

/** One drill-down of the view: its dimension and the server-resolved row label. */
export interface DrillFrame {
  dimension: string
  label: string
}

/**
 * The view's drill-downs, outermost first. At the top level the server's single breadcrumb
 * names the grouping, so the query's top-level `drill` keys tell the two apart; drills saved
 * inside a `return` frame belong to an earlier view.
 */
export function drillFrames(projection: ViewProjection): DrillFrame[] {
  const drills = new URLSearchParams(projection.canonical_query).getAll('drill')
  if (drills.length === 0) return []
  return projection.breadcrumbs ?? []
}
