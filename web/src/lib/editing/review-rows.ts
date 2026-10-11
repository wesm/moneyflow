import type { components } from '../api/schema'

type ReviewOperation = components['schemas']['ReviewOperation']
type ReviewTarget = components['schemas']['ReviewTarget']

/** The column of an affected row that shows the field an operation changes. */
export type ReviewRowField = 'merchant' | 'category' | 'visibility'

/** One affected row's changed field, from the row's value before the operation to after it. */
export interface ReviewRowChange {
  field: ReviewRowField
  before: string
  after: string
}

const rowFields: Record<string, ReviewRowField> = {
  'merchant.label': 'merchant',
  'merchant.merge': 'merchant',
  'merchant.reassign': 'merchant',
  'category.assign': 'category',
  'category.create': 'category',
  'category.label': 'category',
  'category.merge': 'category',
  'category.delete': 'category',
  'transaction.hide-toggle': 'visibility',
}

/**
 * The row field `operation` changes for `target`, or undefined when it changes no field the row
 * shows, such as a category's group, or leaves the row's value as it was.
 */
export function reviewRowChange(
  operation: ReviewOperation,
  target: ReviewTarget,
): ReviewRowChange | undefined {
  const field = rowFields[operation.type]
  if (!field || !operation.after) return undefined
  const before = rowValue(field, target)
  return before === operation.after ? undefined : { field, before, after: operation.after }
}

function rowValue(field: ReviewRowField, target: ReviewTarget): string {
  if (field === 'visibility') return target.hidden ? 'Hidden' : 'Visible'
  return target[field]
}
