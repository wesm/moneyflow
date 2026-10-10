import type { components } from '../api/schema'
import type { InsightsEntry } from '../insights/cache.svelte'
import { formatMinor } from '../money/format'
import { tenthsPercent } from '../table/cells'

type Change = components['schemas']['InsightsChange']

export type KpiTone = 'favorable' | 'unfavorable' | 'neutral'

/** A change against the comparison range, such as `▲ 4.1%`, colored by its tone. */
export interface KpiChange {
  readonly text: string
  /** What a screen reader hears instead of the glyphs, such as `up 4.1% vs Aug`. */
  readonly spoken: string
  readonly tone: KpiTone
}

export interface KpiTile {
  readonly key: 'income' | 'spending' | 'net' | 'savings' | 'average' | 'transactions'
  readonly label: string
  readonly value: string
  readonly change?: KpiChange
  /** A quiet line under the value, such as `12 hidden · 3 bank-pending`. */
  readonly detail?: string
}

/**
 * The KPI strip's tiles for an insights entry, following the spec's Metrics table. Changes show
 * only with a comparison label, so All shows none. A drill shows Avg per transaction in place of
 * the savings rate; the server omits the average when no transaction is visible, which shows `—`.
 */
export function kpiTiles(
  entry: InsightsEntry,
  comparisonLabel: string | undefined,
  drill: boolean,
): KpiTile[] {
  const { scale, summary } = entry.response
  const kpis = summary?.kpis
  if (kpis === undefined) return []
  const { scope } = kpis
  const compared = comparisonLabel !== undefined
  const money = (minor: string) => formatMinor(minor, scale)
  const changeOf = (value: Change | undefined) => {
    const change = compared && value !== undefined && priorChange(value, scale, comparisonLabel)
    return change ? { change } : {}
  }
  const tiles: KpiTile[] = [
    {
      key: 'income',
      label: 'Income',
      value: dashZero(scope.in, money),
      ...changeOf(kpis.income_change),
    },
    {
      key: 'spending',
      label: 'Spending',
      value: dashZero(scope.out, money),
      ...changeOf(kpis.spending_change),
    },
    {
      key: 'net',
      label: 'Net',
      value: unsignedZero(scope.net, money),
      ...changeOf(kpis.net_change),
    },
  ]
  if (kpis.savings_rate_tenths !== undefined) {
    tiles.push({
      key: 'savings',
      label: 'Savings rate',
      value: tenthsPercent(kpis.savings_rate_tenths),
    })
  } else if (drill || kpis.average_per_transaction !== undefined) {
    const average = kpis.average_per_transaction
    const value = average === undefined ? '—' : unsignedZero(average, money)
    tiles.push({ key: 'average', label: 'Avg per transaction', value })
  }
  const counts = [
    scope.hidden_count > 0 ? `${count(scope.hidden_count)} hidden` : '',
    // Transactions the bank still lists as pending, not pending edits.
    scope.pending_count > 0 ? `${count(scope.pending_count)} bank-pending` : '',
  ].filter(Boolean)
  const detail = counts.length > 0 ? { detail: counts.join(' · ') } : {}
  tiles.push({ key: 'transactions', label: 'Transactions', value: count(scope.count), ...detail })
  return tiles
}

/**
 * The spec's vs prior display rules, shared by the KPI changes and the table's vs prior column.
 * `label` names the comparison, such as `vs Aug`. An amount change without its amount shows
 * nothing rather than an invented zero.
 */
export function priorChange(change: Change, scale: number, label: string): KpiChange | undefined {
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

/** In and Out show a dash for zero. */
function dashZero(minor: string, money: (minor: string) => string): string {
  return minor === '0' ? '—' : money(minor)
}

/** Net shows zero without the server's `+`, as the table's Net column does. */
function unsignedZero(minor: string, money: (minor: string) => string): string {
  return minor === '0' ? money(minor).slice(1) : money(minor)
}

function count(value: number): string {
  return value.toLocaleString('en-US')
}
