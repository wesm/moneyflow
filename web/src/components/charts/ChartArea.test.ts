import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ChartArea from './ChartArea.svelte'
import type { InsightsBody, InsightsResponse, ViewProjection } from '../../lib/api/client'
import { InsightsCache } from '../../lib/insights/cache.svelte'
import { testInsights } from '../../test/insights'
import { testProjection } from '../../test/projection'

interface Call {
  body: InsightsBody
  resolve(response: InsightsResponse): void
  reject(error: unknown): void
}

function harness() {
  const calls: Call[] = []
  const cache = new InsightsCache(
    (body) => new Promise((resolve, reject) => calls.push({ body, resolve, reject })),
    vi.fn(),
  )
  return { cache, calls }
}

function view(query = 'v=1', revision = '1'): ViewProjection {
  const base = testProjection()
  return testProjection({
    canonical_query: query,
    revision,
    view: { ...base.view, currency: { currency: 'USD', scale: 2 } },
  })
}

function september(call: Call | undefined, revision = '1'): InsightsResponse {
  if (!call) throw new Error('no insights request was sent')
  const response = testInsights({ query: call.body.query, revision })
  response.summary!.kpis = {
    scope: {
      in: '841072',
      out: '-584418',
      net: '256654',
      count: 1204,
      hidden_count: 0,
      pending_count: 0,
    },
    net_change: { kind: 'percent', percent_tenths: 123, grew: true, favorable: true },
  }
  return response
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

describe('ChartArea', () => {
  afterEach(() => cleanup())

  it('holds skeletons in place while the first insights load', () => {
    const { cache } = harness()
    const projection = view()
    cache.sync(projection, [])
    render(ChartArea, { projection, cache, chartsOn: true })
    const row = screen.getByRole('region', { name: 'Charts' })
    expect(row.getAttribute('aria-busy')).toBe('true')
    expect(screen.queryByText('Income')).toBeNull()
    expect(document.querySelectorAll('.mf-kpi')).toHaveLength(5)
  })

  it('shows the KPI strip with the comparison once, and the chart row only when on', async () => {
    const { cache, calls } = harness()
    const projection = view()
    cache.sync(projection, [])
    calls[0]?.resolve(september(calls[0]))
    await settle()
    const { rerender } = render(ChartArea, { projection, cache, chartsOn: true })

    const income = screen.getByText('Income')
    expect(income.tagName).toBe('DT')
    expect(income.nextElementSibling?.textContent).toBe('+8,410.72')
    const change = screen.getByText('▲ 12.3%')
    expect(change.closest('dd')?.previousElementSibling?.textContent).toBe('+2,566.54')
    expect(change.closest('[aria-hidden="true"]')).not.toBeNull()
    expect(change.closest('dd')?.textContent).toContain('up 12.3% vs Aug')
    expect(screen.getAllByText('vs Aug')).toHaveLength(1)
    expect(screen.getByRole('region', { name: 'Charts' }).getAttribute('aria-busy')).toBe('false')

    await rerender({ projection, cache, chartsOn: false })
    expect(screen.queryByRole('region', { name: 'Charts' })).toBeNull()
    expect(screen.getByText('Income')).not.toBeNull()
  })

  it('dims the previous view and puts it out of reach until the new one arrives', async () => {
    const { cache, calls } = harness()
    cache.sync(view(), [])
    calls[0]?.resolve(september(calls[0]))
    await settle()
    const next = view('v=2')
    cache.sync(next, [])
    render(ChartArea, { projection: next, cache, chartsOn: true })

    const content = screen.getByText('Income').closest<HTMLElement>('.mf-chart-states')!
    expect(content.classList.contains('mf-chart-states--stale')).toBe(true)
    expect(content.inert).toBe(true)
    const row = screen.getByRole('region', { name: 'Charts', hidden: true })
    expect(row.getAttribute('aria-busy')).toBe('true')

    calls[1]?.resolve(september(calls[1]))
    await settle()
    expect(content.inert).toBe(false)
    expect(row.getAttribute('aria-busy')).toBe('false')
  })

  it('shows a dash for Avg per transaction in a drill with no visible transaction', async () => {
    const { cache, calls } = harness()
    const projection = view('v=1&drill=merchant%3AUSD%3A2%3Aexample-key')
    cache.sync(projection, [])
    calls[0]?.resolve(testInsights({ query: calls[0].body.query, revision: '1' }))
    await settle()
    render(ChartArea, { projection, cache, chartsOn: true })
    expect(screen.getByText('Avg per transaction').nextElementSibling?.textContent).toBe('—')
  })

  it('offers Retry after a failure without a live announcement', async () => {
    const { cache, calls } = harness()
    const projection = view()
    cache.sync(projection, [])
    render(ChartArea, { projection, cache, chartsOn: true })
    calls[0]?.reject(new Error('insights unavailable'))
    await settle()

    const failure = screen.getByText(/Couldn't load charts/)
    expect(failure.closest('[aria-live], [role="alert"], [role="status"]')).toBeNull()
    expect(screen.queryByRole('region', { name: 'Charts' })).toBeNull()

    await fireEvent.click(within(failure).getByRole('button', { name: 'Retry' }))
    expect(calls).toHaveLength(2)
    calls[1]?.resolve(september(calls[1]))
    await settle()
    expect(screen.queryByText(/Couldn't load charts/)).toBeNull()
    expect(screen.getByText('Income')).not.toBeNull()
  })

  it('keeps the KPI strip when only a row series fails', async () => {
    const { cache, calls } = harness()
    const projection = view()
    cache.sync(
      projection,
      Array.from({ length: 201 }, (_, index) => `merchant:${index}`),
    )
    render(ChartArea, { projection, cache, chartsOn: true })
    calls[0]?.resolve(september(calls[0]))
    calls[1]?.reject(new Error('series unavailable'))
    await settle()

    expect(screen.getByText(/Couldn't load charts/)).not.toBeNull()
    expect(screen.getByText('Income')).not.toBeNull()
  })

  it('reports its height when it resizes, and zero once it is gone', () => {
    const observed: (() => void)[] = []
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(callback: () => void) {
          observed.push(callback)
        }
        observe(): void {}
        disconnect(): void {}
      },
    )
    try {
      const { cache } = harness()
      const measure = vi.fn()
      const { unmount } = render(ChartArea, { projection: view(), cache, chartsOn: true, measure })
      observed[0]?.()
      expect(measure).toHaveBeenLastCalledWith(expect.any(Number))
      unmount()
      expect(measure).toHaveBeenLastCalledWith(0)
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('shows nothing for a view of every currency at once', () => {
    const { cache } = harness()
    render(ChartArea, { projection: testProjection(), cache, chartsOn: true })
    expect(document.querySelector('.mf-chart-area')).toBeNull()
  })
})
