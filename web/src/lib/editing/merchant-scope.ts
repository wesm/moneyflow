import type { ViewProjection } from '../api/client'
import { drillFrames } from '../shell/drills'

/** Whether a merchant edit may rename the whole merchant, and why not when it may not. */
export interface MerchantScope {
  entity: boolean
  reason?: string
}

/**
 * The TUI's rule for whole-merchant scope (`merchantEntityScopePossible`): nothing is selected,
 * and either the cursor is on a merchant row or the view is inside a merchant drill.
 *
 * @param projection The displayed view.
 * @param cursor The cursor's row index.
 * @param selectionCount The profile-wide count of selected rows.
 */
export function merchantScope(
  projection: ViewProjection,
  cursor: number,
  selectionCount: number,
): MerchantScope {
  if (selectionCount > 0) {
    return { entity: false, reason: 'Clear the selection to rename the whole merchant' }
  }
  const row = projection.aggregate_rows?.find((candidate) => candidate.index === cursor)
  const drilled = drillFrames(projection).some((frame) => frame.dimension === 'merchant')
  if (row?.dimension === 'merchant' || drilled) return { entity: true }
  return { entity: false, reason: 'Open a merchant to rename all its transactions' }
}
