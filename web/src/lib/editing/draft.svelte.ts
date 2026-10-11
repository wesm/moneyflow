import type { ViewProjection } from '../api/client'
import type { EditorKind, EditScope } from './editor-copy'

/** What the merchant or category editor opened over. */
export interface EditorOpening {
  kind: EditorKind
  target: { kind: 'transaction' | 'aggregate'; identity: string }
  projection: ViewProjection
  cursorIndex: number
}

/**
 * The merchant or category editor's input. The overlay state holds it, so it survives the editor
 * moving between the popover and the phone sheet when the window crosses 640 px.
 */
export class EditorDraft {
  text = $state('')
  scope = $state<EditScope>('transactions')
  /** The name a `Create "…"` accepted; the group step shows while the text still reads it. */
  created = $state<string>()
  /** The group of a new category. */
  groupId = $state('')
  /** What the editor first opened over; a remount keeps it, so it does not retarget. */
  opened?: EditorOpening
}
