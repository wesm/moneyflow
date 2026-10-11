import { trapFocus } from '@kenn-io/kit-ui/utils/focus-trap'

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

/**
 * Focuses the control a click landed on. WebKit does not focus a button on click, and an overlay
 * records `document.activeElement` as its opener when it opens, so a click handler calls this
 * first to make the overlay return focus to the control.
 */
export function focusClicked(event: MouseEvent): void {
  if (event.currentTarget instanceof HTMLElement) event.currentTarget.focus({ preventScroll: true })
}

const TABBABLE = ['a[href]', 'button', 'input', 'select', 'textarea', '[tabindex]']
  .map((selector) => `${selector}:not(:disabled):not([tabindex="-1"])`)
  .join(', ')

/** Whether Tab can reach `item`: tabbable, laid out, visible, and outside an inert subtree. */
function reachable(item: HTMLElement): boolean {
  if (!item.matches(TABBABLE) || item.getClientRects().length === 0) return false
  if (item.closest('[inert]')) return false
  return getComputedStyle(item).visibility !== 'hidden'
}

/** The control after `active` in `items`, or before it with `back`, wrapping at the ends. */
function nextTabbable(items: HTMLElement[], active: Element | null, back: boolean): HTMLElement {
  const follows = (item: HTMLElement) =>
    !!active && !!(active.compareDocumentPosition(item) & Node.DOCUMENT_POSITION_FOLLOWING)
  if (back) return items.findLast((item) => !follows(item) && item !== active) ?? items.at(-1)!
  return items.find(follows) ?? items[0]!
}

/**
 * kit `trapFocus`, except that the surface moves every Tab and Shift+Tab itself. kit's trap
 * moves focus only past the first and last control and leaves the rest to the browser. WebKit's
 * Tab skips buttons and links unless macOS keyboard navigation is on, so from the surface or a
 * button it went to the page behind the dialog (kit-ui upstream gap). An Attachment.
 */
export function trapEveryTab(surface: HTMLElement): () => void {
  const keydown = (event: KeyboardEvent) => {
    if (event.key !== 'Tab' || event.defaultPrevented) return
    event.preventDefault()
    // Browsers return a selector list's matches in document order; jsdom returns them by group.
    const items = Array.from(surface.querySelectorAll<HTMLElement>('*')).filter(reachable)
    if (items.length === 0) return surface.focus()
    nextTabbable(items, document.activeElement, event.shiftKey).focus()
  }
  // Added before kit's handler, which then sees each Tab already taken.
  surface.addEventListener('keydown', keydown)
  const release = trapFocus(surface)
  return () => {
    surface.removeEventListener('keydown', keydown)
    release()
  }
}
