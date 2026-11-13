import type { RowKind } from './pointer'

/** Desktop row height in pixels. CSS reads it as `--mf-row-height`, so virtualization agrees. */
export const ROW_HEIGHT = 30
/** Two-line phone row height in pixels, at 640 px and narrower. */
export const COMPACT_ROW_HEIGHT = 52
/** A grouped phone row has room for a third line, where In and Out go when they do not fit. */
export const COMPACT_GROUP_ROW_HEIGHT = 56

export function rowHeight(compact: boolean, kind: RowKind): number {
  if (!compact) return ROW_HEIGHT
  return kind === 'aggregate' ? COMPACT_GROUP_ROW_HEIGHT : COMPACT_ROW_HEIGHT
}

/**
 * The first and last rows that sit wholly inside a viewport `viewport` px tall whose top is
 * `scrollTop` px below the first row. A viewport shorter than a row still names one row.
 */
export function visibleRange(
  scrollTop: number,
  viewport: number,
  rowHeight: number,
  total: number,
): { first: number; last: number } {
  if (total <= 0) return { first: 0, last: -1 }
  const first = Math.min(total - 1, Math.ceil(Math.max(0, scrollTop) / rowHeight))
  const bottom = Math.floor((scrollTop + viewport) / rowHeight) - 1
  return { first, last: Math.min(total - 1, Math.max(first, bottom)) }
}

/** Where a scroller shows the table: its scroll position and height, and the grid's top in it. */
export interface RowPort {
  scrollTop: number
  viewport: number
  rowHeight: number
  gridTop: number
}

/**
 * The `scrollTop` that shows row `index` whole below the sticky one-row header, or the current
 * one when the row already shows. Row 0 scrolls to the very top, so the room above the table
 * shows too.
 */
export function revealScrollTop(index: number, port: RowPort): number {
  const { scrollTop, viewport, rowHeight, gridTop } = port
  const top = gridTop + rowHeight * (index + 1)
  if (top < scrollTop + rowHeight) return index === 0 ? 0 : top - rowHeight
  if (top + rowHeight > scrollTop + viewport) return top + rowHeight - viewport
  return scrollTop
}
