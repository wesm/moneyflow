import type { ViewProjection } from '../api/client'
import type { LocalAction } from '../shortcuts'
import { availableCapabilities, unavailableReason } from '../shell/capabilities'

export type EditAction = Extract<
  LocalAction,
  'edit.merchant' | 'edit.category' | 'edit.hide' | 'edit.delete'
>

/** A button that runs what a key runs: one of the edits, Info, or the selection bar's Clear. */
export interface ActionButton<Action> {
  action: Action
  label: string
  key: string
  /** Why the button is disabled; absent while it can run. */
  reason?: string
}

/** A row actions sheet item: an edit or Info, for one row. */
export type RowActionButton = ActionButton<EditAction | 'transaction.info'>

/** The edits, with the keys that run them and the capabilities they need. */
export const edits = [
  { action: 'edit.merchant', label: 'Merchant', key: 'm', capability: 'transaction.edit-merchant' },
  { action: 'edit.category', label: 'Category', key: 'c', capability: 'transaction.edit-category' },
  { action: 'edit.hide', label: 'Hide', key: 'h', capability: 'transaction.toggle-hidden' },
  { action: 'edit.delete', label: 'Delete', key: 'x', capability: 'transaction.delete' },
] as const
const info = {
  action: 'transaction.info',
  label: 'Info',
  key: 'i',
  capability: 'transaction.show-info',
} as const

/** Buttons for `entries`. One the view cannot run carries the server's reason. */
export function actionButtons<Action extends string>(
  projection: ViewProjection,
  entries: readonly (ActionButton<Action> & { capability: string })[],
): ActionButton<Action>[] {
  const available = availableCapabilities(projection)
  return entries.map(({ action, label, key, capability }) =>
    available.has(capability)
      ? { action, label, key }
      : { action, label, key, reason: unavailableReason(projection, capability) },
  )
}

/**
 * The row actions sheet's items: the edits, then Info. While a selection exists the edits would
 * change the selection instead of the sheet's row, so they leave that to the selection bar.
 */
export function rowActionButtons(projection: ViewProjection): RowActionButton[] {
  const buttons = actionButtons<RowActionButton['action']>(projection, [...edits, info])
  if (projection.selection_count === 0) return buttons
  return buttons.map((button) =>
    button.action === 'transaction.info'
      ? button
      : { ...button, reason: button.reason ?? 'Clear the selection to edit one transaction' },
  )
}
