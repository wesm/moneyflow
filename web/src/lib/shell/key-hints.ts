import type { ViewProjection } from '../api/client'
import { availableCapabilities } from './capabilities'
import { emptyScope } from './period'

export interface KeyHint {
  key: string
  label: string
}

interface CandidateHint extends KeyHint {
  capability: string
  /** The key explains an unavailable capability's reason, so its hint stays. */
  explains?: true
}

const helpHint: CandidateHint = { capability: 'overlay.help', key: '?', label: 'Help' }

function groupedHints(projection: ViewProjection): CandidateHint[] {
  return [
    { capability: 'view.drill', key: '↵', label: 'Open' },
    {
      capability: 'view.cycle-grouping',
      key: 'g',
      label: projection.view.sub_grouping ? 'Break down' : 'Group',
    },
    { capability: 'overlay.search', key: '/', label: 'Search' },
    { capability: 'time.choose', key: 't', label: 'Period' },
    helpHint,
  ]
}

const detailHints: CandidateHint[] = [
  { capability: 'transaction.edit-merchant', key: 'm', label: 'Merchant' },
  { capability: 'transaction.edit-category', key: 'c', label: 'Category' },
  { capability: 'selection.toggle', key: 'Space', label: 'Select' },
  { capability: 'transaction.show-info', key: 'i', label: 'Info' },
  helpHint,
]

const selectionHints: CandidateHint[] = [
  { capability: 'transaction.edit-merchant', key: 'm', label: 'Merchant' },
  { capability: 'transaction.edit-category', key: 'c', label: 'Category' },
  { capability: 'transaction.toggle-hidden', key: 'h', label: 'Hide' },
  { capability: 'transaction.delete', key: 'x', label: 'Delete', explains: true },
  { capability: 'view.back', key: 'Esc', label: 'Clear' },
]

/** Chooses the status line's key hints for the current view, at most six. */
export function keyHints(projection: ViewProjection, overlayOpen: boolean): KeyHint[] {
  if (overlayOpen) return [{ key: 'Esc', label: 'Close' }]
  const available = availableCapabilities(projection)
  let candidates = groupedHints(projection)
  if (projection.selection_count > 0) candidates = selectionHints
  else if (emptyScope(projection)) {
    // The empty-period state has no rows to open or edit.
    candidates = candidates.filter(({ capability }) => capability !== 'view.drill')
  } else if (projection.view.mode === 'detail') candidates = detailHints
  const explained = new Set(
    (projection.capabilities ?? []).filter(({ reason }) => reason).map(({ id }) => id),
  )
  return candidates
    .filter(
      ({ capability, explains }) =>
        available.has(capability) || (explains && explained.has(capability)),
    )
    .slice(0, 6)
    .map(({ key, label }) => ({ key, label }))
}
