import type { ViewProjection } from '../api/client'

/** One drill-down of the view: its dimension and the server-resolved row label. */
export interface DrillFrame {
  dimension: string
  label: string
}

/**
 * The view's drill-downs, outermost first. At the top level the server's single breadcrumb
 * names the grouping, so `view.drill_depth` tells the two apart.
 */
export function drillFrames(projection: ViewProjection): DrillFrame[] {
  if (projection.view.drill_depth === 0) return []
  return projection.breadcrumbs ?? []
}
