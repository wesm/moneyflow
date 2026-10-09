import type { ViewProjection } from '../api/client'
import type { LocalAction } from '../shortcuts'
import { availableCapabilities, unavailableReason } from '../shell/capabilities'
import { moneyCell } from './cells'

type EditAction = Extract<
  LocalAction,
  'edit.merchant' | 'edit.category' | 'edit.hide' | 'edit.delete'
>

/** A button that runs what a key runs: one of the edits, Info, or the selection bar's Clear. */
interface ActionButton<Action> {
  action: Action
  label: string
  key: string
  /** Why the button is disabled; absent while it can run. */
  reason?: string
}

/** A selection bar button: an edit the keys also run, or Clear. */
export type SelectionButton = ActionButton<EditAction | 'clear'>

/** A row actions sheet item: an edit or Info, for one row. */
export type RowActionButton = ActionButton<EditAction | 'transaction.info'>

const nouns: Record<string, [string, string]> = {
  merchant: ['merchant', 'merchants'],
  category: ['category', 'categories'],
  group: ['group', 'groups'],
  account: ['account', 'accounts'],
  year: ['year', 'years'],
  month: ['month', 'months'],
  day: ['day', 'days'],
}

function counted(count: number, [singular, plural]: [string, string]): string {
  return `${count.toLocaleString('en-US')} ${count === 1 ? singular : plural}`
}

function groupNoun(view: ViewProjection['view']): [string, string] {
  const dimension = view.sub_grouping ?? view.grouping
  const key = dimension === 'time' ? view.time_granularity : dimension
  return nouns[key] ?? ['group', 'groups']
}

/**
 * Describes the selection: the selected groups in a grouped view, the affected transactions, and
 * their Net in the active currency, then the Net of any other currency with its code. Null
 * without a selection.
 */
export function selectionText(projection: ViewProjection): string | null {
  const count = projection.selection_count
  if (count === 0) return null
  const detail = projection.view.mode === 'detail'
  const scope = projection.view.currency
  const summary = projection.selection_summary ?? []
  const active = summary.find(
    (entry) => entry.currency === scope?.currency && entry.scale === scope.scale,
  )
  const parts = detail ? [] : [counted(count, groupNoun(projection.view))]
  const transactions = active?.count ?? (detail ? count : undefined)
  if (transactions !== undefined) parts.push(counted(transactions, ['transaction', 'transactions']))
  if (active) parts.push(moneyCell(active.net, 'unsigned'))
  for (const entry of summary) {
    if (entry !== active) parts.push(`${moneyCell(entry.net, 'unsigned')} ${entry.currency}`)
  }
  return parts.join(' · ')
}

const edits = [
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
function actionButtons<Action extends string>(
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

/** The bar's buttons: the edits, then Clear, which is always enabled. */
export function selectionButtons(projection: ViewProjection): SelectionButton[] {
  const buttons: SelectionButton[] = actionButtons(projection, edits)
  return [...buttons, { action: 'clear', label: 'Clear', key: 'Esc' }]
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
