import { cleanup, render } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'

import KpiStrip from './KpiStrip.svelte'
import type { KpiTile } from '../../lib/charts/kpis'

const fourTiles: KpiTile[] = [
  { key: 'income', label: 'Income', value: '+100.00' },
  { key: 'spending', label: 'Spending', value: '-80.00' },
  { key: 'net', label: 'Net', value: '+20.00' },
  { key: 'transactions', label: 'Transactions', value: '3' },
]

describe('KpiStrip', () => {
  afterEach(() => cleanup())

  it('draws five skeleton tiles before any insights arrive', () => {
    render(KpiStrip, { tiles: undefined, comparisonLabel: undefined })
    expect(document.querySelectorAll('.mf-kpi')).toHaveLength(5)
  })

  it('draws as many skeleton tiles as the strip last showed', async () => {
    const { rerender } = render(KpiStrip, { tiles: fourTiles, comparisonLabel: undefined })
    expect(document.querySelectorAll('.mf-kpi')).toHaveLength(4)

    await rerender({ tiles: undefined, comparisonLabel: undefined })
    expect(document.querySelectorAll('.mf-kpi[aria-hidden="true"]')).toHaveLength(4)
  })
})
