import type { ViewProjection } from '../api/client'
import { actionButtons, edits, type ActionButton, type EditAction } from './actions'
import { moneyCell } from './cells'

/** A selection bar button: an edit the keys also run, or Clear. */
export type SelectionButton = ActionButton<EditAction | 'clear'>

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
  // A drill broken down by a sub-grouping keeps the detail mode but shows groups.
  const detail = projection.view.mode === 'detail' && !projection.view.sub_grouping
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

/** The bar's buttons: the edits, then Clear, which is always enabled. */
export function selectionButtons(projection: ViewProjection): SelectionButton[] {
  const buttons: SelectionButton[] = actionButtons(projection, edits)
  return [...buttons, { action: 'clear', label: 'Clear', key: 'Esc' }]
}
