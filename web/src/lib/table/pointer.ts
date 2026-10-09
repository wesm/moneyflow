export type RowKind = 'transaction' | 'aggregate'

/** What a double-click does: drill, open Info, or edit one of the row's fields. */
export type DoubleClickAction = 'drill' | 'information' | 'merchant' | 'category'

/** A pointer event's row index and the column key of the cell it landed in. */
export interface PointerHit {
  index: number
  column: string | undefined
}

/**
 * The spec's pointer rules for a double-click. Grouped rows drill from any cell. Transaction rows
 * edit Merchant or Category from those cells and open Info elsewhere. The selection box toggles
 * on each click, so a double-click there does nothing more.
 */
export function doubleClickAction(
  kind: RowKind,
  column: string | undefined,
): DoubleClickAction | undefined {
  if (column === 'select') return undefined
  if (kind === 'aggregate') return 'drill'
  if (column === 'merchant' || column === 'category') return column
  return 'information'
}

/** Finds the body row and cell under a pointer event target; header rows carry no index. */
export function pointerHit(target: EventTarget | null): PointerHit | undefined {
  if (!(target instanceof Element)) return undefined
  const row = target.closest<HTMLElement>('[data-index]')
  if (!row) return undefined
  const column = target.closest<HTMLElement>('[data-column]')?.dataset['column']
  return { index: Number(row.dataset['index']), column }
}

/**
 * Takes the kit button inside `node` out of the Tab order. kit IconButton has no `tabindex`, and
 * the grid is one Tab stop: its keys reach every row action.
 */
export function outOfTabOrder(node: HTMLElement): void {
  node.querySelector('button')?.setAttribute('tabindex', '-1')
}
