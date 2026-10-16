/** The provider a profile reads its transactions from. */
export type ProviderKind = 'monarch' | 'ynab' | 'simplefin' | 'amazon' | 'local' | 'csv'

/** Explore views show transactions; maintain views open a tool over the current view. */
export type ViewGroup = 'explore' | 'maintain'

export type ViewID =
  | 'merchant'
  | 'category'
  | 'group'
  | 'account'
  | 'time'
  | 'detail'
  | 'duplicates'
  | 'export'
  | 'categories'
  | 'groups'
  | 'amazon'

export interface ViewInfo {
  id: ViewID
  label: string
  group: ViewGroup
}

const views: ViewInfo[] = [
  { id: 'merchant', label: 'Merchants', group: 'explore' },
  { id: 'category', label: 'Categories', group: 'explore' },
  { id: 'group', label: 'Groups', group: 'explore' },
  { id: 'account', label: 'Accounts', group: 'explore' },
  { id: 'time', label: 'Time', group: 'explore' },
  { id: 'detail', label: 'All transactions', group: 'explore' },
  { id: 'duplicates', label: 'Duplicates', group: 'maintain' },
  { id: 'export', label: 'Export', group: 'maintain' },
  { id: 'categories', label: 'Edit categories', group: 'maintain' },
  { id: 'groups', label: 'Edit groups', group: 'maintain' },
  { id: 'amazon', label: 'Import Amazon orders', group: 'maintain' },
]

/** The views a profile of this kind offers, in the order moneyflow's sidebar lists them. */
export function viewsFor(kind: ProviderKind): ViewInfo[] {
  return views.filter((view) => view.id !== 'amazon' || kind === 'amazon')
}
