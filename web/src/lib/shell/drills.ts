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
  if (!drilled(projection.canonical_query)) return []
  return projection.breadcrumbs ?? []
}

/** Whether a canonical view query drills into a row, by its top-level `drill` keys. */
export function drilled(query: string): boolean {
  return new URLSearchParams(query).has('drill')
}
