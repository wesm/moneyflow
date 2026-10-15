import type { ViewProjection } from '../api/client'
import type { Dimension, LocalAction } from '../shortcuts'
import { availableCapabilities, unavailableReason } from './capabilities'
import { drillFrames } from './drills'

/** What a navigation entry does: the same handler its key reaches. */
export type NavRun =
  | { kind: 'apply'; action: 'view.set-grouping'; dimension: Dimension }
  | { kind: 'apply'; action: 'view.show-detail' }
  | { kind: 'local'; action: LocalAction }

export interface NavItem {
  id: string
  label: string
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
  maintenance('duplicates', 'Duplicates', 'view.find-duplicates', 'view.duplicates'),
  maintenance('export', 'Export', 'transactions.export', 'transactions.export'),
  maintenance('categories', 'Edit categories', 'category.manage', 'manage.categories'),
  maintenance('groups', 'Edit groups', 'category-group.manage', 'manage.groups'),
]

const amazonEntry = maintenance(
  'amazon',
  'Import Amazon orders',
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
      active: spec.active ?? false,
      available: offered,
      reason: offered ? undefined : unavailableReason(projection, spec.capability),
      run: spec.run,
    }
  }
  const allTransactions = projection.view.mode === 'detail' && drillFrames(projection).length === 0
  const explore = groupings.map(({ dimension, label }) =>
    entry({
      id: dimension,
      label,
      capability: 'view.set-grouping',
      run: { kind: 'apply', action: 'view.set-grouping', dimension },
      active: !allTransactions && projection.view.grouping === dimension,
    }),
  )
  explore.push(
    entry({
      id: 'detail',
      label: 'All transactions',
      capability: 'view.show-detail',
      run: { kind: 'apply', action: 'view.show-detail' },
      active: allTransactions,
    }),
  )
  const maintain = maintainEntries.map(entry)
  if (projection.profile_kind === 'amazon') maintain.push(entry(amazonEntry))
  return { explore, maintain }
}

/** Reports whether the profile has a provider to refresh from the sidebar's provider status. */
export function hasProviderStatus(kind: ViewProjection['profile_kind']): boolean {
  return kind === 'monarch' || kind === 'ynab' || kind === 'simplefin'
}

function maintenance(
  id: string,
  label: string,
  capability: string,
  action: LocalAction,
): EntrySpec {
  return { id, label, capability, run: { kind: 'local', action } }
}
