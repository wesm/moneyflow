import { describe, expect, it } from 'vitest'

import type { InsightsResponse } from '../api/client'
import type { components } from '../api/schema'
import type { InsightsEntry } from '../insights/cache.svelte'
import { testInsights } from '../../test/insights'
import { kpiTiles } from './kpis'

type KPIs = components['schemas']['InsightsKPIs']
type Totals = components['schemas']['InsightsTotals']
type Change = components['schemas']['InsightsChange']

const september: Totals = {
  in: '841072',
  out: '-584418',
  net: '256654',
  count: 1204,
  hidden_count: 12,
  pending_count: 3,
}

function entry(kpis: Partial<KPIs>, overrides: Partial<InsightsResponse> = {}): InsightsEntry {
  const response = testInsights(overrides)
  return {
    query: response.query,
    revision: response.revision,
    response: {
      ...response,
      summary: { ...response.summary!, kpis: { scope: september, ...kpis } },
    },
    series: new Map(),
  }
}

function change(overrides: Partial<Change>): Change {
  return { kind: 'percent', percent_tenths: 0, grew: false, favorable: true, ...overrides }
}

function tile(tiles: ReturnType<typeof kpiTiles>, key: string) {
  const found = tiles.find((candidate) => candidate.key === key)
  if (!found) throw new Error(`no ${key} tile`)
  return found
}

describe('kpiTiles', () => {
  it('shows the scope totals with exact money', () => {
    const tiles = kpiTiles(entry({ savings_rate_tenths: 305 }), 'vs Aug')
    expect(tiles.map(({ label, value }) => [label, value])).toEqual([
      ['Income', '+8,410.72'],
      ['Spending', '-5,844.18'],
      ['Net', '+2,566.54'],
      ['Savings rate', '30.5%'],
      ['Transactions', '1,204'],
    ])
  })

  it('shows a dash for zero Income and Spending, and Net zero unsigned', () => {
    const scope = { ...september, in: '0', out: '0', net: '0', count: 0 }
    const tiles = kpiTiles(entry({ scope }), undefined)
    expect(tile(tiles, 'income').value).toBe('—')
    expect(tile(tiles, 'spending').value).toBe('—')
    expect(tile(tiles, 'net').value).toBe('0.00')
  })

  it('shows the savings rate, negative too, only when the server sends one', () => {
    expect(tile(kpiTiles(entry({ savings_rate_tenths: -125 }), 'vs Aug'), 'savings').value).toBe(
      '-12.5%',
    )
    const keys = kpiTiles(entry({}), 'vs Aug').map((candidate) => candidate.key)
    expect(keys).toEqual(['income', 'spending', 'net', 'transactions'])
  })

  it('shows Avg per transaction in place of the savings rate inside a drill', () => {
    const tiles = kpiTiles(entry({ average_per_transaction: '-9000' }), 'vs Aug')
    expect(tiles.map((candidate) => candidate.key)).toEqual([
      'income',
      'spending',
      'net',
      'average',
      'transactions',
    ])
    expect(tile(tiles, 'average')).toMatchObject({ label: 'Avg per transaction', value: '-90.00' })
  })

  it('formats money at the response scale', () => {
    const scope = { ...september, in: '1500', out: '-20', net: '1480' }
    const tiles = kpiTiles(entry({ scope }, { scale: 0 }), undefined)
    expect(tile(tiles, 'income').value).toBe('+1,500')
    expect(tile(tiles, 'spending').value).toBe('-20')
  })

  it('counts transactions and appends hidden and pending only above zero', () => {
    const tiles = kpiTiles(entry({}), undefined)
    expect(tile(tiles, 'transactions')).toMatchObject({
      value: '1,204',
      detail: '12 hidden · 3 pending',
    })
    const plain = { ...september, count: 1, hidden_count: 0, pending_count: 0 }
    expect(tile(kpiTiles(entry({ scope: plain }), undefined), 'transactions').detail).toBe(
      undefined,
    )
    const pendingOnly = { ...september, hidden_count: 0 }
    expect(tile(kpiTiles(entry({ scope: pendingOnly }), undefined), 'transactions').detail).toBe(
      '3 pending',
    )
  })

  it('shows changes as arrows and percents colored by favorable', () => {
    const tiles = kpiTiles(
      entry({
        income_change: change({ percent_tenths: 41, grew: true, favorable: true }),
        spending_change: change({ percent_tenths: 200, grew: true, favorable: false }),
        net_change: change({ percent_tenths: 123, grew: false, favorable: false }),
      }),
      'vs Aug',
    )
    expect(tile(tiles, 'income').change).toEqual({ text: '▲ 4.1%', tone: 'favorable' })
    expect(tile(tiles, 'spending').change).toEqual({ text: '▲ 20.0%', tone: 'unfavorable' })
    expect(tile(tiles, 'net').change).toEqual({ text: '▼ 12.3%', tone: 'unfavorable' })
  })

  it('shows new, a signed amount on a sign change, and no arrow for an unchanged percent', () => {
    const tiles = kpiTiles(
      entry({
        income_change: change({ kind: 'new', favorable: true }),
        spending_change: change({ percent_tenths: 0, favorable: true }),
        net_change: change({ kind: 'amount', amount: '46000', favorable: true }),
      }),
      'vs Aug',
    )
    expect(tile(tiles, 'income').change).toEqual({ text: 'new', tone: 'favorable' })
    expect(tile(tiles, 'spending').change).toEqual({ text: '0.0%', tone: 'favorable' })
    expect(tile(tiles, 'net').change).toEqual({ text: '+460.00', tone: 'favorable' })
  })

  it('shows a dash when both periods are zero', () => {
    const tiles = kpiTiles(entry({ net_change: change({ kind: 'none' }) }), 'vs Aug')
    expect(tile(tiles, 'net').change).toEqual({ text: '—', tone: 'neutral' })
  })

  it('hides every change without a comparison, as for All', () => {
    const kpis = { net_change: change({ percent_tenths: 50, grew: true }) }
    const tiles = kpiTiles(entry(kpis), undefined)
    expect(tiles.every((candidate) => candidate.change === undefined)).toBe(true)
  })
})
