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

const nextTimeUnit: Record<string, string> = { year: 'Month', month: 'Day', day: 'Year' }

/** `t` groups by Time, or in a Time view names the unit its next press shows, as the TUI does. */
function timeHint(projection: ViewProjection): string {
  const { mode, grouping, sub_grouping, time_granularity } = projection.view
  const timeView = sub_grouping
    ? sub_grouping === 'time'
    : mode === 'aggregate' && grouping === 'time'
  return (timeView && nextTimeUnit[time_granularity]) || 'Time'
}

function groupedHints(projection: ViewProjection): CandidateHint[] {
  return [
    { capability: 'view.drill', key: '↵', label: 'Open' },
    {
      capability: 'view.cycle-grouping',
      key: 'g',
      label: projection.view.sub_grouping ? 'Break down' : 'Group',
    },
    { capability: 'time.toggle-granularity', key: 't', label: timeHint(projection) },
    { capability: 'overlay.search', key: '/', label: 'Search' },
    { capability: 'time.choose', key: 'p', label: 'Period' },
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

/** Chooses the status line's key hints for the current view. */
export function keyHints(projection: ViewProjection, overlayOpen: boolean): KeyHint[] {
  if (overlayOpen) return [{ key: 'Esc', label: 'Close' }]
  const available = availableCapabilities(projection)
  let candidates = groupedHints(projection)
  if (projection.selection_count > 0) candidates = selectionHints
  else if (emptyScope(projection)) {
    // The empty-period state has no rows to open or edit.
    candidates = candidates.filter(({ capability }) => capability !== 'view.drill')
  } else if (projection.view.mode === 'detail' && !projection.view.sub_grouping) {
    candidates = detailHints
  }
  const explained = new Set(
    (projection.capabilities ?? []).filter(({ reason }) => reason).map(({ id }) => id),
  )
  return candidates
    .filter(
      ({ capability, explains }) =>
        available.has(capability) || (explains && explained.has(capability)),
    )
    .map(({ key, label }) => ({ key, label }))
}
