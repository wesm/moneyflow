import type { InsightsResponse, InsightsSeries } from '../lib/api/client'

/** A synthetic insights response for one month scope in USD, with a summary and no series. */
export function testInsights(overrides: Partial<InsightsResponse> = {}): InsightsResponse {
  return {
    version: '1',
    revision: '0',
    query: 'v=1',
    currency: 'USD',
    scale: 2,
    today: '2026-10-07',
    latest: '2026-10-07',
    ranges: {
      kind: 'month',
      scope: { start: '2026-09-01', end: '2026-09-30' },
      context_granularity: 'month',
      context: [],
      trend: [],
      comparison: { start: '2026-08-01', end: '2026-08-31' },
      comparison_label: 'vs Aug',
    },
    summary: {
      kpis: {
        scope: { in: '0', out: '0', net: '0', count: 0, hidden_count: 0, pending_count: 0 },
      },
      cashflow: { labels: [], in: [], out: [], net: [], count: [] },
    },
    series: [],
    ...overrides,
  }
}

/** A row series with one trend month and one chart-context period. */
export function testSeries(identity: string, net = '-100'): InsightsSeries {
  return { identity, trend: [net], context: { in: ['0'], out: [net] } }
}
