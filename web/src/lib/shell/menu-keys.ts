/**
 * Keeps keys inside an open kit dropdown in `wrapper`. kit FilterDropdown and SelectDropdown
 * handle only their own keys, so a letter or arrow would also reach the window shortcuts and
 * change the view behind the open menu. Escape and Tab still pass: kit closes the menu on
 * Escape at the document, and Tab leaves it.
 */
export function containMenuKeys(wrapper: HTMLElement): () => void {
  const keydown = (event: KeyboardEvent) => {
    if (event.key === 'Escape' || event.key === 'Tab') return
    if (wrapper.querySelector('.kit-popover-card, [aria-expanded="true"]')) {
      event.stopPropagation()
    }
  }
  wrapper.addEventListener('keydown', keydown)
  return () => wrapper.removeEventListener('keydown', keydown)
}
