import type { components } from '../api/schema'

type Money = components['schemas']['Money']
type AggregateRow = components['schemas']['AggregateRow']

/** Formats an integer count of tenths of a percent, such as 62 as `6.2%`. */
function tenthsPercent(tenths: number): string {
  const sign = tenths < 0 ? '-' : ''
  const magnitude = Math.abs(tenths)
  return `${sign}${Math.trunc(magnitude / 10)}.${magnitude % 10}%`
}

function moneySign(money: Money): -1 | 0 | 1 {
  if (money.minor === '0') return 0
  return money.minor.startsWith('-') ? -1 : 1
}

/**
 * The server's display for an amount. A zero shows as a dash (In and Out) or without its sign
 * (Net), because the server formats zero as `+0.00`.
 */
export function moneyCell(money: Money, zero: '—' | 'unsigned'): string {
  if (money.minor !== '0') return money.display
  return zero === '—' ? '—' : money.display.replace(/^[+-]/, '')
}

/** The Share label and its bar length as a display fraction from 0 to 1. */
export function shareCell(row: AggregateRow): { label: string; ratio: number } {
  if (row.total.minor === '0') return { label: '—', ratio: 0 }
  return { label: tenthsPercent(row.share_tenths), ratio: row.share_tenths / 1000 }
}

/** Merchants show their top category and its percent; categories show their group. */
export function contextCell(row: AggregateRow, dimension: string): string {
  if (dimension === 'merchant' && row.top_category) {
    return `${row.top_category} ${row.top_category_percent ?? 0}%`
  }
  if (dimension === 'category') return row.group ?? ''
  return ''
}

export function savingsCell(row: AggregateRow): string {
  return row.savings_rate_tenths === undefined ? '—' : tenthsPercent(row.savings_rate_tenths)
}

/**
 * The diverging Time Net bar: which side of the center line it extends to and its length as a
 * percent of the cell. `ratio` is the server's `net_ratio`, from -10000 to 10000.
 */
export function netBar(ratio: number | undefined): { side: 'in' | 'out'; percent: number } | null {
  if (!ratio) return null
  return { side: ratio > 0 ? 'in' : 'out', percent: Math.abs(ratio) / 200 }
}

/**
 * Names the group that starts at `index` when rows sorted by Net change sign. A zero Net row
 * belongs to neither group. The first loaded row has no neighbor to compare with.
 */
export function incomeDivider(
  rows: readonly AggregateRow[],
  index: number,
): 'Income' | 'Spending' | null {
  const row = rows[index]
  const previous = rows[index - 1]
  if (!row || !previous) return null
  const sign = moneySign(row.total)
  if (sign === 0 || sign === moneySign(previous.total)) return null
  return sign > 0 ? 'Income' : 'Spending'
}
