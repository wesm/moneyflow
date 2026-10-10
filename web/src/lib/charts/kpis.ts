import type { components } from '../api/schema'
import type { InsightsEntry } from '../insights/cache.svelte'
import { formatMinor } from '../money/format'
import { tenthsPercent } from '../table/cells'

type Change = components['schemas']['InsightsChange']

export type KpiTone = 'favorable' | 'unfavorable' | 'neutral'

/** A change against the comparison range, such as `▲ 4.1%`, colored by its tone. */
export interface KpiChange {
  readonly text: string
  readonly tone: KpiTone
}

export interface KpiTile {
  readonly key: 'income' | 'spending' | 'net' | 'savings' | 'average' | 'transactions'
  readonly label: string
  readonly value: string
  readonly change?: KpiChange
  /** A quiet line under the value, such as `12 hidden · 3 pending`. */
  readonly detail?: string
}

/**
 * The KPI strip's tiles for an insights entry, following the spec's Metrics table. Changes show
 * only with a comparison label, so All shows none.
 */
export function kpiTiles(entry: InsightsEntry, comparisonLabel: string | undefined): KpiTile[] {
  const { scale, summary } = entry.response
  const kpis = summary?.kpis
  if (kpis === undefined) return []
  const { scope } = kpis
  const compared = comparisonLabel !== undefined
  const money = (minor: string) => formatMinor(minor, scale)
  const changeOf = (value: Change | undefined) =>
    compared && value !== undefined ? { change: kpiChange(value, scale) } : {}
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
  } else if (kpis.average_per_transaction !== undefined) {
    const value = unsignedZero(kpis.average_per_transaction, money)
    tiles.push({ key: 'average', label: 'Avg per transaction', value })
  }
  const counts = [
    scope.hidden_count > 0 ? `${count(scope.hidden_count)} hidden` : '',
    scope.pending_count > 0 ? `${count(scope.pending_count)} pending` : '',
  ].filter(Boolean)
  const detail = counts.length > 0 ? { detail: counts.join(' · ') } : {}
  tiles.push({ key: 'transactions', label: 'Transactions', value: count(scope.count), ...detail })
  return tiles
}

/** The spec's vs prior display rules, shared by the KPI changes. */
function kpiChange(change: Change, scale: number): KpiChange {
  const tone = change.favorable ? 'favorable' : 'unfavorable'
  switch (change.kind) {
    case 'none':
      return { text: '—', tone: 'neutral' }
    case 'new':
      return { text: 'new', tone }
    case 'amount':
      return { text: formatMinor(change.amount ?? '0', scale), tone }
    case 'percent': {
      const percent = tenthsPercent(change.percent_tenths)
      // A change that rounds to 0.0% has no direction worth an arrow.
      if (change.percent_tenths === 0) return { text: percent, tone }
      return { text: `${change.grew ? '▲' : '▼'} ${percent}`, tone }
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
