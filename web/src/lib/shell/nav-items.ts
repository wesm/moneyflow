import type { ViewProjection } from '../api/client'
import type { Dimension, LocalAction } from '../shortcuts'
import { availableCapabilities, unavailableReason } from './capabilities'

/** What a navigation entry does: the same handler its key reaches. */
export type NavRun =
  | { kind: 'apply'; action: 'view.set-grouping'; dimension: Dimension }
  | { kind: 'apply'; action: 'view.show-detail' }
  | { kind: 'local'; action: LocalAction }

export interface NavItem {
  id: string
  label: string
  key: string
  active: boolean
  available: boolean
  /** Why the entry is unavailable; undefined while it is available. */
  reason: string | undefined
  run: NavRun
}

export interface NavSections {
  explore: NavItem[]
  maintain: NavItem[]
}

interface EntrySpec {
  id: string
  label: string
  key: string
  capability: string
  run: NavRun
  active?: boolean
}

const groupings: Array<{ dimension: Dimension; label: string }> = [
  { dimension: 'merchant', label: 'Merchants' },
  { dimension: 'category', label: 'Categories' },
  { dimension: 'group', label: 'Groups' },
  { dimension: 'account', label: 'Accounts' },
  { dimension: 'time', label: 'Time' },
]

const maintainEntries: EntrySpec[] = [
  maintenance('duplicates', 'Duplicates', 'D', 'view.find-duplicates', 'view.duplicates'),
  maintenance('export', 'Export', 'E', 'transactions.export', 'transactions.export'),
  maintenance('categories', 'Edit categories', 'C', 'category.manage', 'manage.categories'),
  maintenance('groups', 'Edit groups', 'G', 'category-group.manage', 'manage.groups'),
]

const amazonEntry = maintenance(
  'amazon',
  'Import Amazon orders',
  'r',
  'provider.refresh',
  'provider.refresh',
)

/** Builds the sidebar's Explore and Maintain entries for the current view. */
export function navItems(projection: ViewProjection): NavSections {
  const available = availableCapabilities(projection)
  const entry = (spec: EntrySpec): NavItem => {
    const offered = available.has(spec.capability)
    return {
      id: spec.id,
      label: spec.label,
      key: spec.key,
      active: spec.active ?? false,
      available: offered,
      reason: offered ? undefined : unavailableReason(projection, spec.capability),
      run: spec.run,
    }
  }
  const allTransactions = projection.view.mode === 'detail' && !isDrilled(projection)
  const explore = groupings.map(({ dimension, label }, index) =>
    entry({
      id: dimension,
      label,
      key: String(index + 1),
      capability: 'view.set-grouping',
      run: { kind: 'apply', action: 'view.set-grouping', dimension },
      active: !allTransactions && projection.view.grouping === dimension,
    }),
  )
  explore.push(
    entry({
      id: 'detail',
      label: 'All transactions',
      key: 'd',
      capability: 'view.show-detail',
      run: { kind: 'apply', action: 'view.show-detail' },
      active: allTransactions,
    }),
  )
  const maintain = maintainEntries.map(entry)
  if (projection.profile_kind === 'amazon') maintain.push(entry(amazonEntry))
  return { explore, maintain }
}

/** Converts a displayed key ("D") to its `aria-keyshortcuts` value ("Shift+D"). */
export function ariaKeyshortcuts(key: string): string {
  return /^[A-Z]$/.test(key) ? `Shift+${key}` : key
}

/** Reports whether the profile has a provider to refresh from the sidebar's provider status. */
export function hasProviderStatus(kind: ViewProjection['profile_kind']): boolean {
  return kind === 'monarch' || kind === 'ynab' || kind === 'simplefin'
}

function maintenance(
  id: string,
  label: string,
  key: string,
  capability: string,
  action: LocalAction,
): EntrySpec {
  return { id, label, key, capability, run: { kind: 'local', action } }
}

/**
 * Reports whether the current view sits inside a drill. The canonical query lists the current
 * view's drills as top-level `drill` parameters; return frames nest theirs inside `return`.
 */
function isDrilled(projection: ViewProjection): boolean {
  return new URLSearchParams(projection.canonical_query).has('drill')
}
