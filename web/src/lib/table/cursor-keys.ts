/** A cursor move one of the table's keys asks for, as the TUI binds them. */
export type CursorMove = 'up' | 'down' | 'home' | 'end' | 'page-up' | 'page-down'

const moves: Record<string, CursorMove> = {
  ArrowUp: 'up',
  k: 'up',
  ArrowDown: 'down',
  j: 'down',
  Home: 'home',
  T: 'home',
  End: 'end',
  B: 'end',
  PageUp: 'page-up',
  PageDown: 'page-down',
}

/** Rows the table last showed whole, which a paging key moves by. The table keeps it current. */
let pageRows = 1

export function setPageRows(rows: number): void {
  pageRows = Math.max(1, rows)
}

export function currentPageRows(): number {
  return pageRows
}

/**
 * The cursor move `event` asks for, or none. Keys held with Ctrl, Alt, or Meta belong to the
 * browser or to other shortcuts. Lowercase `t` and `b` are not moves: `t` chooses the period.
 */
export function cursorMove(
  event: Pick<KeyboardEvent, 'key' | 'ctrlKey' | 'altKey' | 'metaKey'>,
): CursorMove | undefined {
  if (event.ctrlKey || event.altKey || event.metaKey) return undefined
  return Object.hasOwn(moves, event.key) ? moves[event.key] : undefined
}

/** The row `move` puts a cursor on `cursor` at, in a result of `total` rows, `page` rows a page. */
export function cursorTarget(
  move: CursorMove,
  cursor: number,
  total: number,
  page: number,
): number {
  const last = Math.max(total - 1, 0)
  if (move === 'home') return 0
  if (move === 'end') return last
  const step = move === 'up' || move === 'down' ? 1 : Math.max(1, page)
  const direction = move === 'up' || move === 'page-up' ? -1 : 1
  return Math.min(Math.max(cursor + direction * step, 0), last)
}
