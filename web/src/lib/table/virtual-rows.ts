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
