import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ChartArea from './ChartArea.svelte'
import type { InsightsBody, InsightsResponse, ViewProjection } from '../../lib/api/client'
import { InsightsCache } from '../../lib/insights/cache.svelte'
import { testInsights } from '../../test/insights'
import { testAggregateRow, testProjection } from '../../test/projection'

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

const interaction = { cursorIndex: 0, onperiod: vi.fn() }

/** Merchants A and B scoped to September 2026, with one chart-context month. */
function merchants(): ViewProjection {
  const base = view()
  const projection: ViewProjection = {
    ...base,
    view: { ...base.view, mode: 'aggregate' },
    aggregate_rows: [
      testAggregateRow({ identity: 'merchant:a', index: 0, label: 'Merchant A' }),
      testAggregateRow({ identity: 'merchant:b', index: 1, label: 'Merchant B' }),
    ],
  }
  delete projection.detail_rows
  return projection
}

function withSeries(call: Call | undefined): InsightsResponse {
  const response = september(call)
  response.ranges.context = [
    {
      start: '2026-09-01',
      end: '2026-09-30',
      partial: false,
      absent: false,
      period: { granularity: 'month', year: 2026, month: 9 },
    },
  ]
  response.summary!.cashflow = {
    labels: ['Sep 2026'],
    in: ['841072'],
    out: ['-584418'],
    net: ['256654'],
    count: [1204],
  }
  response.series = ['merchant:a', 'merchant:b'].map((identity) => ({
    identity,
    trend: ['-100'],
    context: { in: ['0'], out: ['-100'] },
  }))
  return response
}

/** A Time grouping of Aug and Sep 2026 whose insights hold each month's group shares. */
async function timeGrouping() {
  const { cache, calls } = harness()
  const base = merchants()
  const months = [8, 9].map((month) => ({ granularity: 'month', year: 2026, month }))
  const projection: ViewProjection = {
    ...base,
    aggregate_rows: months.map((period, index) =>
      testAggregateRow({ identity: `time:${index}`, index, label: `Month ${index}`, period }),
    ),
  }
  cache.sync(projection, [])
  const response = september(calls[0])
  response.ranges.context = months.map((period) => ({
    start: `2026-0${period.month}-01`,
    end: `2026-0${period.month}-28`,
    partial: false,
    absent: false,
    period,
  }))
  response.summary!.cashflow = {
    labels: ['Aug 2026', 'Sep 2026'],
    in: ['0', '0'],
    out: ['0', '0'],
    net: ['0', '0'],
    count: [0, 0],
  }
  response.summary!.period_composition = ['Food', 'Travel'].map((label) => ({
    spending: [{ key: label, label, net: '-100', share_tenths: 1000 }],
    income: [],
  }))
  calls[0]?.resolve(response)
  await settle()
  return { projection, cache, calls }
}

describe('ChartArea', () => {
  afterEach(() => cleanup())

  it('holds skeletons in place while the first insights load', () => {
    const { cache } = harness()
    const projection = view()
    cache.sync(projection, [])
    render(ChartArea, { projection, cache, chartsOn: true, ...interaction })
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
    const { rerender } = render(ChartArea, { projection, cache, chartsOn: true, ...interaction })

    const income = screen.getByText('Income')
    expect(income.tagName).toBe('DT')
    expect(income.nextElementSibling?.textContent).toBe('+8,410.72')
    const change = screen.getByText('▲ 12.3%')
    expect(change.closest('dd')?.previousElementSibling?.textContent).toBe('+2,566.54')
    expect(change.closest('[aria-hidden="true"]')).not.toBeNull()
    expect(change.closest('dd')?.textContent).toContain('up 12.3% vs Aug')
    expect(screen.getAllByText('vs Aug')).toHaveLength(1)
    expect(screen.getByRole('region', { name: 'Charts' }).getAttribute('aria-busy')).toBe('false')

    await rerender({ projection, cache, chartsOn: false, ...interaction })
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
    render(ChartArea, { projection: next, cache, chartsOn: true, ...interaction })

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
    render(ChartArea, { projection, cache, chartsOn: true, ...interaction })
    expect(screen.getByText('Avg per transaction').nextElementSibling?.textContent).toBe('—')
  })

  it('offers Retry after a failure without a live announcement', async () => {
    const { cache, calls } = harness()
    const projection = view()
    cache.sync(projection, [])
    render(ChartArea, { projection, cache, chartsOn: true, ...interaction })
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
    render(ChartArea, { projection, cache, chartsOn: true, ...interaction })
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
      const { unmount } = render(ChartArea, {
        projection: view(),
        cache,
        chartsOn: true,
        measure,
        ...interaction,
      })
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
    render(ChartArea, { projection: testProjection(), cache, chartsOn: true, ...interaction })
    expect(document.querySelector('.mf-chart-area')).toBeNull()
  })

  it('moves the solid cursor columns from cached series without another request', async () => {
    const { cache, calls } = harness()
    const projection = merchants()
    cache.sync(projection, ['merchant:a', 'merchant:b'])
    calls[0]?.resolve(withSeries(calls[0]))
    await settle()
    const onperiod = vi.fn()
    const props = { projection, cache, chartsOn: true, onperiod }
    const { rerender } = render(ChartArea, { ...props, cursorIndex: 0 })
    const chart = () => document.querySelector('[data-cursor]')?.getAttribute('data-cursor')
    expect(chart()).toBe('merchant:a')

    await rerender({ ...props, cursorIndex: 1 })
    expect(chart()).toBe('merchant:b')
    expect(calls).toHaveLength(1)

    const group = screen.getByRole('group', { name: 'Cash flow' })
    await fireEvent.click(within(group).getByRole('button', { name: /^Sep 2026, in \+8,410\.72/ }))
    expect(onperiod).toHaveBeenCalledWith({ start: '2026-09-01', end: '2026-09-30' })
  })

  it('shows the chart failure line, not a broken page, for a misaligned cash flow', async () => {
    const { cache, calls } = harness()
    const projection = view()
    cache.sync(projection, [])
    const response = september(calls[0])
    response.summary!.cashflow = {
      labels: ['Sep 2026'],
      in: ['0'],
      out: ['0'],
      net: ['0'],
      count: [0],
    }
    calls[0]?.resolve(response)
    await settle()
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    render(ChartArea, { projection, cache, chartsOn: true, ...interaction })
    expect(screen.getByText(/Couldn't load charts/)).not.toBeNull()
    expect(screen.getByText('Income')).not.toBeNull()
    expect(screen.queryByRole('group', { name: 'Cash flow' })).toBeNull()
  })

  it('shows where it went for a grouped view, and the switch asks for nothing', async () => {
    const { cache, calls } = harness()
    const projection = merchants()
    cache.sync(projection, ['merchant:a', 'merchant:b'])
    const response = withSeries(calls[0])
    response.summary!.composition = {
      spending: [
        { key: 'a', identity: 'merchant:a', label: 'Merchant A', net: '-300', share_tenths: 600 },
        { key: 'b', identity: 'merchant:b', label: 'Merchant B', net: '-200', share_tenths: 400 },
      ],
      income: [],
    }
    calls[0]?.resolve(response)
    await settle()
    const onrow = vi.fn()
    const ondrill = vi.fn()
    const props = { projection, cache, chartsOn: true, onperiod: vi.fn(), onrow, ondrill }
    const { rerender } = render(ChartArea, { ...props, cursorIndex: 0 })
    const list = screen.getByRole('list', { name: 'Where it went' })
    const bar = (name: string) =>
      within(list).getByRole('button', { name: new RegExp(`^${name},`) })
    expect(bar('Merchant A').classList.contains('mf-bar--cursor')).toBe(true)

    await rerender({ ...props, cursorIndex: 1 })
    expect(bar('Merchant B').classList.contains('mf-bar--cursor')).toBe(true)
    await fireEvent.click(bar('Merchant A'))
    expect(onrow).toHaveBeenCalledWith('merchant:a')
    await fireEvent.dblClick(bar('Merchant A'))
    expect(ondrill).toHaveBeenCalledWith('merchant:a')

    await fireEvent.click(screen.getByRole('radio', { name: 'Income' }))
    expect(screen.getByText('No income')).not.toBeNull()
    expect(calls).toHaveLength(1)
  })

  it('shows the cursor period’s group shares in the Time grouping without a request', async () => {
    const { projection, cache, calls } = await timeGrouping()
    const props = { projection, cache, chartsOn: true, onperiod: vi.fn() }
    const { rerender } = render(ChartArea, { ...props, cursorIndex: 0 })
    const list = () => screen.getByRole('list', { name: /by group$/ })
    expect(list().getAttribute('aria-label')).toBe('Aug 2026 by group')
    expect(within(list()).getByText('Food')).not.toBeNull()
    expect(within(list()).queryAllByRole('button')).toHaveLength(0)

    await rerender({ ...props, cursorIndex: 1 })
    expect(list().getAttribute('aria-label')).toBe('Sep 2026 by group')
    expect(within(list()).getByText('Travel')).not.toBeNull()
    expect(calls).toHaveLength(1)
  })

  it('returns from the Time grouping’s panel to the table on Esc', async () => {
    const { projection, cache } = await timeGrouping()
    const onleave = vi.fn()
    render(ChartArea, { projection, cache, chartsOn: true, ...interaction, onleave })
    const spending = screen.getByRole('radio', { name: 'Spending' })
    const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    spending.dispatchEvent(event)
    expect(onleave).toHaveBeenCalledTimes(1)
    // Claimed, so the shell's Esc does not also go back a level.
    expect(event.defaultPrevented).toBe(true)
  })

  it('shows the calendar heatmap in a transaction view, outlining the cursor’s day', async () => {
    const { cache, calls } = harness()
    const projection = view()
    cache.sync(projection, [])
    const response = september(calls[0])
    response.ranges.heatmap = { layout: 'months', start: '2024-01-01', end: '2024-01-31' }
    response.summary!.calendar = {
      in: Array.from({ length: 31 }, () => '0'),
      out: Array.from({ length: 31 }, (_, index) => (index === 1 ? '-1234' : '0')),
    }
    calls[0]?.resolve(response)
    await settle()
    render(ChartArea, { projection, cache, chartsOn: true, ...interaction })
    const group = screen.getByRole('group', { name: 'Daily spending' })
    // The test projection's one transaction is on Jan 2, 2024.
    const day = within(group).getByRole('button', { name: 'Jan 2, 2024, out -12.34' })
    expect(day.classList.contains('mf-day--cursor')).toBe(true)
    expect(screen.queryByRole('list', { name: 'Where it went' })).toBeNull()
  })

  it('shows the failure line in the panel, not a broken page, for a misaligned calendar', async () => {
    const { cache, calls } = harness()
    const projection = view()
    cache.sync(projection, [])
    const response = september(calls[0])
    response.ranges.heatmap = { layout: 'months', start: '2024-01-01', end: '2024-01-31' }
    response.summary!.calendar = { in: ['0'], out: ['0'] }
    calls[0]?.resolve(response)
    await settle()
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    render(ChartArea, { projection, cache, chartsOn: true, ...interaction })
    expect(screen.getByText(/Couldn't load charts/)).not.toBeNull()
    expect(screen.queryByRole('group', { name: 'Daily spending' })).toBeNull()
    expect(screen.getByRole('group', { name: 'Cash flow' })).not.toBeNull()
  })
})
