import type { ViewProjection } from '../api/client'
import type { LocalAction } from '../shortcuts'
import { availableCapabilities, unavailableReason } from '../shell/capabilities'
import { moneyCell } from './cells'

/** A selection bar button: an edit the keys also run, or Clear. */
export interface SelectionButton {
  action:
    | Extract<LocalAction, 'edit.merchant' | 'edit.category' | 'edit.hide' | 'edit.delete'>
    | 'clear'
  label: string
  key: string
  /** Why the button is disabled; absent while it can run. */
  reason?: string
}

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

/** The bar's buttons. An edit the view cannot run carries the server's reason. */
export function selectionButtons(projection: ViewProjection): SelectionButton[] {
  const available = availableCapabilities(projection)
  const buttons: SelectionButton[] = edits.map(({ action, label, key, capability }) =>
    available.has(capability)
      ? { action, label, key }
      : { action, label, key, reason: unavailableReason(projection, capability) },
  )
  return [...buttons, { action: 'clear', label: 'Clear', key: 'Esc' }]
}
