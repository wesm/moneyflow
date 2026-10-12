import type { PendingSummary } from '../api/client'

function count(value: number, singular: string, plural: string): string {
  return `${value} ${value === 1 ? singular : plural}`
}

/** Lists the phrases of the pending summary: edits, affected transactions, and any redo. */
export function pendingParts(pending: PendingSummary): string[] {
  const parts = [
    count(pending.active_operations, 'pending edit', 'pending edits'),
    count(pending.affected_transactions, 'transaction', 'transactions'),
  ]
  if (pending.inactive_operations > 0) parts.push(`${pending.inactive_operations} redo`)
  return parts
}

/** Describes pending profile changes, e.g. "3 pending edits · 27 transactions". */
export function pendingText(pending: PendingSummary): string {
  return pendingParts(pending).join(' · ')
}
