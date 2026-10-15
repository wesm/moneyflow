import { cleanup, render } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'

import TrendCell from './TrendCell.svelte'
import type { components } from '../../lib/api/schema'
import type { RowSeries } from '../../lib/table/trend'
import { testInsights } from '../../test/insights'

type Month = components['schemas']['InsightsPeriodRange']

function month(start: string, flags: Partial<Month> = {}): Month {
  const last = new Date(Date.UTC(2026, Number(start.slice(5, 7)), 0)).getUTCDate()
  return {
    start,
    end: `${start.slice(0, 8)}${last}`,
    period: { granularity: 'month', year: 2026, month: Number(start.slice(5, 7)) },
    absent: false,
    partial: false,
    ...flags,
  }
}

function ready(trend: string[], months: Month[]): RowSeries {
  const response = testInsights()
  return {
    state: 'ready',
    series: { identity: 'merchant:a', trend, context: { in: [], out: [] } },
    response: { ...response, ranges: { ...response.ranges, trend: months } },
  }
}

describe('TrendCell', () => {
  afterEach(cleanup)

  it('draws the Net line over a zero line, hidden from assistive technology', () => {
    const { container } = render(TrendCell, {
      series: ready(
        ['-100', '-300', '200'],
        [month('2026-08-01'), month('2026-09-01'), month('2026-10-01')],
      ),
    })
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('aria-hidden')).toBe('true')
    expect(container.textContent?.trim()).toBe('')
    expect(container.querySelectorAll('.mf-trend__zero')).toHaveLength(1)
    const lines = container.querySelectorAll('polyline')
    expect(lines).toHaveLength(1)
    expect(lines[0]!.getAttribute('points')!.split(' ')).toHaveLength(3)
  })

  it('leaves absent months out and ends a partial last month with a lighter dot', () => {
    const { container } = render(TrendCell, {
      series: ready(
        ['0', '-300', '-50'],
        [
          month('2026-08-01', { absent: true }),
          month('2026-09-01'),
          month('2026-10-01', { partial: true }),
        ],
      ),
    })
    expect(container.querySelector('polyline')!.getAttribute('points')!.split(' ')).toHaveLength(2)
    const end = container.querySelector('.mf-trend__end')!
    expect(end.classList.contains('mf-trend__end--partial')).toBe(true)
    // The picture stays hidden; the cell says in text that its last month runs to date.
    expect(container.querySelector('svg title')?.textContent).toBe('Oct 2026 (to date)')
    expect(container.querySelector('.kit-sr-only')?.textContent).toBe('Oct 2026 (to date)')
  })

  it('shows a skeleton while the series loads and nothing after a failure', () => {
    const loading = render(TrendCell, { series: { state: 'loading' } })
    expect(loading.container.querySelector('.mf-skeleton')).not.toBeNull()
    cleanup()
    const failed = render(TrendCell, { series: { state: 'failed' } })
    expect(failed.container.querySelector('.mf-skeleton, svg')).toBeNull()
  })
})
