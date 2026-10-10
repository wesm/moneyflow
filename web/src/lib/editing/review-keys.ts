/** Controls that act on their own Enter, so the same Enter must not also commit. */
const OWN_ENTER =
  'button, a[href], input, select, textarea, [contenteditable], [role="button"], [role="link"]'

/**
 * What an Enter does in the review drawer:
 * - `commit` commits the reviewed changes;
 * - `swallow` prevents the key and does nothing else, so a focused Commit is not activated;
 * - `pass` leaves the key alone.
 */
export type ReviewEnter = 'commit' | 'swallow' | 'pass'

/**
 * Decides what an Enter does in the review drawer `panel`. Enter commits when focus is in the
 * drawer and not on a control with its own Enter, such as a button, link, or field; Commit itself
 * counts as the drawer. While the first review loads, focus may still be on the page or the table,
 * so an Enter there commits too. A held Enter's repeats and a modified Enter commit nothing.
 */
export function reviewEnter(
  event: KeyboardEvent,
  panel: Element | undefined,
  commit: Element | undefined,
  loading: boolean,
): ReviewEnter {
  if (event.key !== 'Enter' || event.defaultPrevented || !panel) return 'pass'
  const target = event.target
  if (!(target instanceof Element)) return 'pass'
  if (panel.contains(target)) {
    if (target !== commit && target.closest(OWN_ENTER)) return 'pass'
  } else if (!(loading && (target === document.body || target.closest('[role="grid"]')))) {
    return 'pass'
  }
  if (event.repeat || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) {
    return 'swallow'
  }
  return 'commit'
}
