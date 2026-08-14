import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import VisualizationRail from './VisualizationRail.svelte'
import { testProjection } from '../test/projection'

describe('VisualizationRail', () => {
  afterEach(cleanup)

  it('chooses detail summaries and exposes a no-chart empty state', () => {
    const callbacks = { oncursor: vi.fn(), ondrill: vi.fn() }
    render(VisualizationRail, {
      projection: testProjection({ chart: { summary: [] } }),
      cursorIndex: 0,
      ...callbacks,
    })
    expect(screen.getByText('No chart data')).not.toBeNull()
  })

  it('chooses aggregate and time charts from server view metadata', () => {
    const callbacks = { oncursor: vi.fn(), ondrill: vi.fn() }
    const aggregate = testProjection({
      detail_rows: null,
      aggregate_rows: [aggregateRow('merchant', undefined)],
      chart: { marks: [chartMark()] },
    })
    const { unmount } = render(VisualizationRail, {
      projection: aggregate,
      cursorIndex: 0,
      ...callbacks,
    })
    expect(screen.getByText('Aggregate totals by current table order.')).not.toBeNull()
    unmount()
    render(VisualizationRail, {
      projection: {
        ...aggregate,
        view: { ...aggregate.view, grouping: 'time' },
        aggregate_rows: [aggregateRow('time', { granularity: 'month', year: 2026, month: 1 })],
      },
      cursorIndex: 0,
      ...callbacks,
    })
    expect(screen.getByText('Chronological spending by period.')).not.toBeNull()
  })
})

function aggregateRow(
  dimension: string,
  period: { granularity: string; year: number; month: number } | undefined,
) {
  return {
    index: 0,
    identity: 'mark-1',
    dimension,
    label: 'Example',
    count: 1,
    total: chartMark().amount,
    ...(period === undefined ? {} : { period }),
    share_tenths: 1000,
    flags: { selected: false, hidden: false, pending: false },
  }
}

function chartMark() {
  return {
    index: 0,
    identity: 'mark-1',
    label: 'Example',
    amount: {
      minor: '-100',
      currency: 'USD',
      scale: 2,
      decimal: '-1.00',
      display: '-$1.00',
    },
    plot_ratio: -10000,
  }
}
