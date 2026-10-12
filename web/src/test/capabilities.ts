import type { ViewProjection } from '../lib/api/client'

export type Capability = NonNullable<ViewProjection['capabilities']>[number]

/** The capabilities behind the sidebar and rail entries, pending review, and provider refresh. */
export const navCapabilityIDs = [
  'view.set-grouping',
  'view.show-detail',
  'view.find-duplicates',
  'transactions.export',
  'category.manage',
  'category-group.manage',
  'changes.review',
  'provider.refresh',
]

/** Builds capabilities for `ids`, available unless listed in `unavailable`. */
export function testCapabilities(ids: string[], unavailable: string[] = []): Capability[] {
  return ids.map((id) => ({
    id,
    description: id,
    category: 'Views',
    available: !unavailable.includes(id),
  }))
}
