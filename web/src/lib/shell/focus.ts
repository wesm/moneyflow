/** The index of the table row holding `element`, if it is inside one. */
export function rowIndex(element: HTMLElement | undefined): number | undefined {
  const row = element?.closest<HTMLElement>('[data-index]')
  return row ? Number(row.dataset['index']) : undefined
}

/**
 * Returns focus to the control that opened an editing overlay. While the opener is on the page
 * it takes focus. When it left with its row, the same control in the row now at `index`, clamped
 * to the last row, takes it. Otherwise the caller falls back to the grid.
 *
 * @param opener The control that opened the overlay.
 * @param index The opener's row index when the overlay opened, from `rowIndex`.
 * @param grid The element holding the table's rows.
 * @returns Whether focus moved.
 */
export function restoreFocus(
  opener: HTMLElement | undefined,
  index: number | undefined,
  grid: HTMLElement | undefined,
): boolean {
  if (opener?.isConnected) {
    opener.focus({ preventScroll: true })
    return true
  }
  // A removed row keeps its subtree, so the detached opener still names its cell.
  const column = opener?.closest<HTMLElement>('[data-column]')?.dataset['column']
  if (index === undefined || !grid || !column) return false
  let row: Element | undefined
  for (const candidate of grid.querySelectorAll<HTMLElement>('[role="row"][data-index]')) {
    if (Number(candidate.dataset['index']) <= index) row = candidate
  }
  const control = row?.querySelector<HTMLElement>(`[data-column="${column}"] button`)
  control?.focus({ preventScroll: true })
  return control !== undefined && control !== null
}
