import type { InsightsBody, ViewProjection } from '../api/client'

/** The server's limit on row identities in one insights request: one loaded table window. */
const maxInsightsRows = 200

/**
 * The aggregate row identities whose series the charts and table read: the loaded window and the
 * cached windows next to it, each once. Only merchant, category, group, and account rows have
 * series, so the Time grouping and transaction views list none.
 */
export function seriesRows(
  projection: ViewProjection,
  neighbors: readonly ViewProjection[],
): string[] {
  const view = projection.view
  const dimension = view.sub_grouping ?? (view.mode === 'aggregate' ? view.grouping : undefined)
  if (dimension === undefined || dimension === 'time') return []
  const identities = new Set<string>()
  for (const window of [projection, ...neighbors]) {
    for (const row of window.aggregate_rows ?? []) identities.add(row.identity)
  }
  return [...identities]
}

/** The rows, each once, whose series are neither loaded nor on their way in `pending`. */
export function missingRows(
  rows: readonly string[],
  loaded: ReadonlyMap<string, unknown> | undefined,
  pending: readonly (readonly string[])[],
): string[] {
  const skip = new Set<string>(loaded?.keys())
  for (const requested of pending) for (const row of requested) skip.add(row)
  const missing: string[] = []
  for (const row of rows) {
    if (skip.has(row)) continue
    skip.add(row)
    missing.push(row)
  }
  return missing
}

/**
 * The requests that load `rows`, at most 200 each, and the summary when `summary` is set. The
 * first request carries the summary, so the charts can draw before every series arrives.
 */
export function insightsBodies(
  query: string,
  rows: readonly string[],
  summary: boolean,
): InsightsBody[] {
  const bodies: InsightsBody[] = []
  for (let start = 0; start < rows.length; start += maxInsightsRows) {
    const chunk = rows.slice(start, start + maxInsightsRows)
    bodies.push({ query, rows: chunk, summary: summary && start === 0 })
  }
  if (summary && bodies.length === 0) bodies.push({ query, summary: true })
  return bodies
}
