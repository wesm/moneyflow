import type { components } from '../api/schema'
import type { InsightsEntry } from '../insights/cache.svelte'
import { priorChange, type PriorChange } from '../insights/change'
import { formatMinor } from '../money/format'
import { tenthsPercent } from '../table/cells'

type Change = components['schemas']['InsightsChange']

export interface KpiTile {
  readonly key: 'income' | 'spending' | 'net' | 'savings' | 'average' | 'transactions'
  readonly label: string
  readonly value: string
  readonly change?: PriorChange
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
