import type { ViewProjection } from '../api/client'
import type { Dimension, LocalAction } from '../shortcuts'

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
  run: NavRun
}

export interface NavSections {
  explore: NavItem[]
  maintain: NavItem[]
}

const groupings: Array<{ dimension: Dimension; label: string }> = [
  { dimension: 'merchant', label: 'Merchants' },
  { dimension: 'category', label: 'Categories' },
  { dimension: 'group', label: 'Groups' },
  { dimension: 'account', label: 'Accounts' },
  { dimension: 'time', label: 'Time' },
]

/** Builds the sidebar's Explore and Maintain entries for the current view. */
export function navItems(projection: ViewProjection): NavSections {
  const available = new Set(
    (projection.capabilities ?? []).filter((item) => item.available).map(({ id }) => id),
  )
  const allTransactions = projection.view.mode === 'detail' && !isDrilled(projection)
  const explore: NavItem[] = groupings.map(({ dimension, label }, index) => ({
    id: dimension,
    label,
    key: String(index + 1),
    active: !allTransactions && projection.view.grouping === dimension,
    available: available.has('view.set-grouping'),
    run: { kind: 'apply', action: 'view.set-grouping', dimension },
  }))
  explore.push({
    id: 'detail',
    label: 'All transactions',
    key: 'd',
    active: allTransactions,
    available: available.has('view.show-detail'),
    run: { kind: 'apply', action: 'view.show-detail' },
  })
  const maintain: NavItem[] = [
    local(
      'duplicates',
      'Duplicates',
      'D',
      available.has('view.find-duplicates'),
      'view.duplicates',
    ),
    local('export', 'Export', 'E', available.has('transactions.export'), 'transactions.export'),
    local(
      'taxonomy',
      'Categories and groups',
      'C/G',
      available.has('category.manage'),
      'manage.categories',
    ),
  ]
  if (projection.profile_kind === 'amazon') {
    maintain.push(
      local(
        'amazon',
        'Import Amazon orders',
        'r',
        available.has('provider.refresh'),
        'provider.refresh',
      ),
    )
  }
  return { explore, maintain }
}

/** Converts a displayed key ("D", "C/G") to its `aria-keyshortcuts` value ("Shift+D", "Shift+C"). */
export function ariaKeyshortcuts(key: string): string {
  const first = key.split('/')[0] ?? key
  return /^[A-Z]$/.test(first) ? `Shift+${first}` : first
}

function local(
  id: string,
  label: string,
  key: string,
  available: boolean,
  action: LocalAction,
): NavItem {
  return { id, label, key, active: false, available, run: { kind: 'local', action } }
}

/**
 * Reports whether the current view sits inside a drill. The canonical query lists the current
 * view's drills as top-level `drill` parameters; return frames nest theirs inside `return`.
 */
function isDrilled(projection: ViewProjection): boolean {
  return new URLSearchParams(projection.canonical_query).has('drill')
}
