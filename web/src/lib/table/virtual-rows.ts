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
 * The row the cursor follows a scroll to: the first fully visible row when the cursor is above
 * the screen, as after scrolling down, or the last when it is below. None while it shows.
 */
export function followRow(
  range: { first: number; last: number },
  cursor: number,
): number | undefined {
  const { first, last } = range
  if (last < first) return undefined
  if (cursor < first) return first
  if (cursor > last) return last
  return undefined
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
