import type { ViewProjection } from '../api/client'
import type { components } from '../api/schema'

export type TableRow = components['schemas']['AggregateRow'] | components['schemas']['DetailRow']

/**
 * The row at `index` of the result, from whichever loaded window holds it: the current window
 * or a cached neighbor. Rows no loaded window holds draw as placeholders.
 */
export function loadedRow(windows: readonly ViewProjection[], index: number): TableRow | undefined {
  for (const loaded of windows) {
    const rows = loaded.detail_rows ?? loaded.aggregate_rows ?? []
    const row = rows[index - loaded.window.offset]
    if (row) return row
  }
  return undefined
}

/**
 * The row whose window the table asks for: the first fully visible row, while it lies outside
 * the loaded window and the cursor row is not fully visible. A cursor on screen keeps its window,
 * so a key or click that moves it into another window is not undone.
 */
export function rowToShow(
  range: { first: number; last: number },
  cursor: number,
  loaded: { offset: number; limit: number },
): number | undefined {
  const { first, last } = range
  if (last < first || (cursor >= first && cursor <= last)) return undefined
  if (first >= loaded.offset && first < loaded.offset + loaded.limit) return undefined
  return first
}

/**
 * Whether the cursor row was fully visible in `before` but not in `after`, as when the viewport
 * shrinks under it because the selection bar opens or grows.
 */
export function coveredCursor(
  before: { first: number; last: number },
  after: { first: number; last: number },
  cursor: number,
): boolean {
  const inside = ({ first, last }: { first: number; last: number }) =>
    cursor >= first && cursor <= last
  return inside(before) && !inside(after)
}
