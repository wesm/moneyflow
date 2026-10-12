import type { components } from '../api/schema'
import { formatMinor, tenthsPercent } from '../money/format'

type Change = components['schemas']['InsightsChange']

export type ChangeTone = 'favorable' | 'unfavorable' | 'neutral'

/** A change against the comparison range, such as `▲ 4.1%`, colored by its tone. */
export interface PriorChange {
  readonly text: string
  /** What a screen reader hears instead of the glyphs, such as `up 4.1% vs Aug`. */
  readonly spoken: string
  readonly tone: ChangeTone
}

/**
 * The spec's vs prior display rules, shared by the KPI changes and the table's vs prior column.
 * `label` names the comparison, such as `vs Aug`. An amount change without its amount shows
 * nothing rather than an invented zero.
 */
export function priorChange(change: Change, scale: number, label: string): PriorChange | undefined {
  const tone = change.favorable ? 'favorable' : 'unfavorable'
  switch (change.kind) {
    case 'none':
      return { text: '—', spoken: `no change ${label}`, tone: 'neutral' }
    case 'new':
      return { text: 'new', spoken: `new ${label}`, tone }
    case 'amount': {
      if (change.amount === undefined) return undefined
      const text = formatMinor(change.amount, scale)
      return { text, spoken: `${text} ${label}`, tone }
    }
    case 'percent': {
      const percent = tenthsPercent(change.percent_tenths)
      // A change that rounds to 0.0% has no direction worth an arrow.
      if (change.percent_tenths === 0) return { text: percent, spoken: `${percent} ${label}`, tone }
      const [arrow, direction] = change.grew ? ['▲', 'up'] : ['▼', 'down']
      return { text: `${arrow} ${percent}`, spoken: `${direction} ${percent} ${label}`, tone }
    }
  }
}
