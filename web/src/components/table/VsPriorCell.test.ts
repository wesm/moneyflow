import { cleanup, render } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'

import VsPriorCell from './VsPriorCell.svelte'
import type { components } from '../../lib/api/schema'
import type { RowSeries } from '../../lib/table/trend'
import { testInsights } from '../../test/insights'

type Change = components['schemas']['InsightsChange']

function ready(change: Partial<Change> | undefined): RowSeries {
  const vs_prior = change && {
    kind: 'percent' as const,
    percent_tenths: 0,
    grew: false,
    favorable: true,
    ...change,
  }
  return {
    state: 'ready',
    series: {
      identity: 'merchant:a',
      trend: [],
      context: { in: [], out: [] },
      ...(vs_prior ? { vs_prior } : {}),
    },
    response: testInsights(),
  }
}

function shown(series: RowSeries) {
  const { container } = render(VsPriorCell, { series })
  const visible = container.querySelector('[aria-hidden="true"]')?.textContent?.trim()
  const spoken = container.querySelector('.kit-sr-only')?.textContent?.trim()
  const tone = container.querySelector('.mf-tone--favorable')
    ? 'favorable'
    : container.querySelector('.mf-tone--unfavorable')
      ? 'unfavorable'
      : 'neutral'
  cleanup()
  return { visible, spoken, tone }
}

describe('VsPriorCell', () => {
  afterEach(cleanup)

  it('follows the spec’s display rules, naming the comparison for screen readers', () => {
    // Online Megastore: Net -360.00 against -300.00 in August.
    expect(shown(ready({ percent_tenths: 200, grew: true, favorable: false }))).toEqual({
      visible: '▲ 20.0%',
      spoken: 'up 20.0% vs Aug',
      tone: 'unfavorable',
    })
    expect(shown(ready({ percent_tenths: 125, grew: false, favorable: true }))).toEqual({
      visible: '▼ 12.5%',
      spoken: 'down 12.5% vs Aug',
      tone: 'favorable',
    })
    expect(shown(ready({ kind: 'new', favorable: false }))).toEqual({
      visible: 'new',
      spoken: 'new vs Aug',
      tone: 'unfavorable',
    })
    expect(shown(ready({ kind: 'none' }))).toEqual({
      visible: '—',
      spoken: 'no change vs Aug',
      tone: 'neutral',
    })
    expect(shown(ready({ kind: 'amount', amount: '46000', favorable: true }))).toEqual({
      visible: '+460.00',
      spoken: '+460.00 vs Aug',
      tone: 'favorable',
    })
  })

  it('shows a skeleton while the series loads and nothing without a change', () => {
    const loading = render(VsPriorCell, { series: { state: 'loading' } })
    expect(loading.container.querySelector('.mf-skeleton')).not.toBeNull()
    cleanup()
    for (const series of [{ state: 'failed' } as const, ready(undefined)]) {
      const { container } = render(VsPriorCell, { series })
      expect(container.textContent?.trim()).toBe('')
      expect(container.querySelector('.mf-skeleton')).toBeNull()
      cleanup()
    }
  })
})
