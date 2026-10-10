/**
 * The anchored editor's key table (spec "Editing" › "Anchored editor"), as a pure function of
 * the key and the editor's state. The combobox and the editor host act on the result.
 */

export type EditorFocus = 'combobox' | 'scope' | 'stage' | 'cancel'

export type EditorKeyAction =
  | 'open'
  | 'move-next'
  | 'move-prev'
  | 'accept'
  | 'accept-and-stage'
  | 'stage'
  | 'close-list'
  | 'cancel'
  | 'none'

export interface KeyLike {
  key: string
  ctrlKey?: boolean
  metaKey?: boolean
  altKey?: boolean
  shiftKey?: boolean
  isComposing?: boolean
  /** Safari reports 229 on the Enter that ends a composition, after `isComposing` clears. */
  keyCode?: number
}

export interface EditorKeyState {
  listOpen: boolean
  valid: boolean
  focus: EditorFocus
}

/**
 * Maps a key press to the editor action the key table assigns it.
 *
 * Tab always returns `none`: focus moves in normal order and never changes scope. Keys the
 * focused control handles natively (typing into an open list, arrows in the scope group, Enter
 * on a button) also return `none`, as do keys during an input method composition, arrows with
 * Ctrl, Cmd, or Alt, and Enter with Shift or Alt.
 */
export function editorKey(event: KeyLike, state: EditorKeyState): EditorKeyAction {
  if (event.isComposing || event.keyCode === 229) return 'none'
  const inCombobox = state.focus === 'combobox'
  const listOpen = inCombobox && state.listOpen
  switch (event.key) {
    case 'Escape':
      return listOpen ? 'close-list' : 'cancel'
    case 'Enter':
      return enterAction(event, state, listOpen)
    case 'ArrowDown':
      return inCombobox && plainArrow(event) ? (listOpen ? 'move-next' : 'open') : 'none'
    case 'ArrowUp':
      return inCombobox && plainArrow(event) ? (listOpen ? 'move-prev' : 'open') : 'none'
    default:
      return inCombobox && !listOpen && isTyping(event) ? 'open' : 'none'
  }
}

function enterAction(event: KeyLike, state: EditorKeyState, listOpen: boolean): EditorKeyAction {
  if (event.shiftKey || event.altKey) return 'none'
  const staging = event.ctrlKey === true || event.metaKey === true
  if (listOpen) return staging ? 'accept-and-stage' : 'accept'
  if (!state.valid) return 'none'
  if (staging || state.focus === 'combobox' || state.focus === 'scope') return 'stage'
  return 'none'
}

function plainArrow(event: KeyLike): boolean {
  return !event.ctrlKey && !event.metaKey && !event.altKey
}

function isTyping(event: KeyLike): boolean {
  if (event.key === 'Backspace' || event.key === 'Delete') return true
  if (event.ctrlKey || event.metaKey) return false
  return Array.from(event.key).length === 1
}
