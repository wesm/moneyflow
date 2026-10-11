import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { tick } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import CashflowChart from './CashflowChart.svelte'
import type { InsightsResponse } from '../../lib/api/client'
import type { InsightsEntry } from '../../lib/insights/cache.svelte'
import { testInsights } from '../../test/insights'

type Context = InsightsResponse['ranges']['context']

const months: [number, string, string][] = [
  [7, 'Jul 2026', '2026-07-31'],
  [8, 'Aug 2026', '2026-08-31'],
  [9, 'Sep 2026', '2026-09-30'],
]

/**
 * Jul through Sep 2026, scoped to September, with Online Megastore's September as its data.
 * `partial` marks September partial, as the month holding today would be.
 */
function chart(partial = false): InsightsEntry {
  const response = testInsights()
  const context: Context = months.map(([month, , end]) => ({
    start: `2026-0${month}-01`,
    end,
    partial: partial && month === 9,
    absent: false,
    period: { granularity: 'month', year: 2026, month },
  }))
  response.ranges = { ...response.ranges, context }
  response.summary!.cashflow = {
    labels: months.map(([, label]) => label),
    in: ['0', '0', '12000'],
    out: ['0', '-30000', '-48000'],
    net: ['0', '-30000', '-36000'],
    count: [0, 1, 4],
  }
  return { query: response.query, revision: response.revision, response, series: new Map() }
}

type ContextPeriod = Context[number]['period']

/** A chart of `periods`, named by their labels, with no amounts, over All time at `revision`. */
function plain(periods: [string, ContextPeriod][], revision: string): InsightsEntry {
  const response = testInsights({ revision })
  const zeros = periods.map(() => '0')
  response.ranges = {
    ...response.ranges,
    kind: 'all',
    context_granularity: periods[0]?.[1].granularity === 'year' ? 'year' : 'month',
    context: periods.map(([, period]) => ({
      ...bounds(period),
      partial: false,
      absent: false,
      period,
    })),
  }
  delete response.ranges.scope
  response.summary!.cashflow = {
    labels: periods.map(([label]) => label),
    in: zeros,
    out: zeros,
    net: zeros,
    count: periods.map(() => 0),
  }
  return { query: response.query, revision, response, series: new Map() }
}

function bounds(period: ContextPeriod): { start: string; end: string } {
  const month = period.month ?? 1
  const last = new Date(Date.UTC(period.year, period.granularity === 'year' ? 12 : month, 0))
  const start = new Date(Date.UTC(period.year, month - 1, 1))
  return { start: start.toISOString().slice(0, 10), end: last.toISOString().slice(0, 10) }
}

const monthsOf = (values: number[]): [string, ContextPeriod][] =>
  values.map((month) => [
    `${['Jun', 'Jul', 'Aug', 'Sep'][month - 6]} 2026`,
    { granularity: 'month', year: 2026, month },
  ])

const cursor = {
  identity: 'merchant-online-megastore',
  label: 'Online Megastore',
  in: ['0', '0', '12000'],
  out: ['0', '-30000', '-48000'],
}

interface Options {
  cursor: typeof cursor
  onleave: () => void
  partial: boolean
}

function setup({ partial = false, ...props }: Partial<Options> = {}) {
  const onperiod = vi.fn()
  render(CashflowChart, { entry: chart(partial), onperiod, ...props })
  const group = screen.getByRole('group', { name: 'Cash flow' })
  const column = (label: string) =>
    within(group).getByRole('button', { name: new RegExp(`^${label}, `) })
  return { onperiod, group, column }
}

describe('CashflowChart', () => {
  afterEach(() => cleanup())

  it('keeps the Tab stop on its period when a new revision adds an earlier one', async () => {
    const { rerender } = render(CashflowChart, {
      entry: plain(monthsOf([7, 8, 9]), '0'),
      onperiod: vi.fn(),
    })
    const stops = () => screen.getAllByRole('button').filter((button) => button.tabIndex === 0)
    const august = screen.getByRole('button', { name: /^Aug 2026, / })
    august.focus()
    await tick()
    expect(stops()).toEqual([august])

    await rerender({ entry: plain(monthsOf([6, 7, 8, 9]), '1') })
    expect(stops()).toEqual([screen.getByRole('button', { name: /^Aug 2026, / })])
  })

  it('keeps one Tab stop when a new revision draws fewer, longer periods', async () => {
    const { rerender } = render(CashflowChart, {
      entry: plain(monthsOf([7, 8, 9]), '0'),
      onperiod: vi.fn(),
    })
    screen.getByRole('button', { name: /^Sep 2026, / }).focus()
    await tick()

    const years = plain(
      [
        ['2025', { granularity: 'year', year: 2025 }],
        ['2026', { granularity: 'year', year: 2026 }],
      ],
      '1',
    )
    await rerender({ entry: years })
    const buttons = screen.getAllByRole('button')
    expect(buttons).toHaveLength(2)
    expect(buttons.filter((button) => button.tabIndex === 0)).toEqual([buttons[1]])
  })

  it('extends a shift-click over All time from the column focused before it', async () => {
    const onperiod = vi.fn()
    render(CashflowChart, { entry: plain(monthsOf([7, 8, 9]), '0'), onperiod })
    const column = (label: string) =>
      screen.getByRole('button', { name: new RegExp(`^${label}, `) })
    column('Jul 2026').focus()

    // A press focuses the column before its click arrives.
    await fireEvent.pointerDown(column('Sep 2026'), { button: 0, shiftKey: true, pointerId: 1 })
    column('Sep 2026').focus()
    await fireEvent.click(column('Sep 2026'), { shiftKey: true })
    expect(onperiod).toHaveBeenCalledExactlyOnceWith({ start: '2026-07-01', end: '2026-09-30' })
  })

  it('names each column with its period, In, Out, and Net', () => {
    const { group } = setup()
    expect(
      within(group)
        .getAllByRole('button')
        .map((button) => button.ariaLabel),
    ).toEqual([
      'Jul 2026, in +0.00, out +0.00, net +0.00',
      'Aug 2026, in +0.00, out -300.00, net -300.00',
      'Sep 2026, in +120.00, out -480.00, net -360.00',
    ])
  })

  it('scopes to a clicked column and to a range on shift-click', async () => {
    const { onperiod, column } = setup()
    await fireEvent.click(column('Aug 2026'))
    expect(onperiod).toHaveBeenLastCalledWith({ start: '2026-08-01', end: '2026-08-31' })
    // September is the scope, so a shift-click on July reaches from July through September.
    await fireEvent.click(column('Jul 2026'), { shiftKey: true })
    expect(onperiod).toHaveBeenLastCalledWith({ start: '2026-07-01', end: '2026-09-30' })
  })

  it('sets a custom range from a drag across columns', async () => {
    const { onperiod, column } = setup()
    await fireEvent.pointerDown(column('Jul 2026'), { button: 0, pointerId: 1 })
    await fireEvent.pointerEnter(column('Aug 2026'), { pointerId: 1 })
    await fireEvent.pointerEnter(column('Sep 2026'), { pointerId: 1 })
    expect(onperiod).not.toHaveBeenCalled()
    await fireEvent.pointerUp(window, { pointerId: 1 })
    expect(onperiod).toHaveBeenCalledExactlyOnceWith({ start: '2026-07-01', end: '2026-09-30' })
  })

  it('is one Tab stop that arrows move through, and Esc leaves it', async () => {
    const onleave = vi.fn()
    const { group, column } = setup({ onleave })
    const stops = within(group)
      .getAllByRole('button')
      .filter((button) => button.tabIndex === 0)
    // The roving stop starts on the scope.
    expect(stops).toEqual([column('Sep 2026')])

    column('Sep 2026').focus()
    await fireEvent.keyDown(column('Sep 2026'), { key: 'ArrowLeft' })
    expect(document.activeElement).toBe(column('Aug 2026'))
    expect(column('Aug 2026').tabIndex).toBe(0)
    expect(column('Sep 2026').tabIndex).toBe(-1)
    await fireEvent.keyDown(column('Aug 2026'), { key: 'Home' })
    expect(document.activeElement).toBe(column('Jul 2026'))

    const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    column('Jul 2026').dispatchEvent(escape)
    expect(onleave).toHaveBeenCalledOnce()
    expect(escape.defaultPrevented).toBe(true)
  })

  it('leaves Esc to the drawer when there is nowhere to leave to', () => {
    const { column } = setup()
    const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    column('Sep 2026').dispatchEvent(escape)
    expect(escape.defaultPrevented).toBe(false)
  })

  it('draws the cursor row solid and shows its amounts in the tooltip', async () => {
    const { group, column } = setup({ cursor })
    expect(group.closest('[data-cursor]')?.getAttribute('data-cursor')).toBe(
      'merchant-online-megastore',
    )
    expect(document.querySelectorAll('.mf-cashflow__cursor').length).toBeGreaterThan(0)

    await fireEvent.focus(column('Sep 2026'))
    await fireEvent.focusIn(column('Sep 2026'))
    const tip = await screen.findByRole('tooltip')
    expect(tip.textContent).toContain('Sep 2026')
    expect(tip.textContent).toContain('+120.00')
    expect(tip.textContent).toContain('-360.00')
    expect(tip.textContent).toContain('Online Megastore')
  })

  it('draws no cursor without one', () => {
    setup()
    expect(document.querySelector('[data-cursor]')).toBeNull()
    expect(document.querySelectorAll('.mf-cashflow__cursor')).toHaveLength(0)
  })

  it('extends the range with Shift+Enter as with a shift-click', async () => {
    const { onperiod, column } = setup()
    await fireEvent.keyDown(column('Jul 2026'), { key: 'Enter', shiftKey: true })
    expect(onperiod).toHaveBeenCalledExactlyOnceWith({ start: '2026-07-01', end: '2026-09-30' })
  })

  it('draws a partial period hollow with the cursor share still solid inside it', () => {
    const { column } = setup({ cursor, partial: true })
    const september = column('Sep 2026')
    expect(september.classList.contains('mf-cashflow__column--partial')).toBe(true)
    expect(column('Aug 2026').classList.contains('mf-cashflow__column--partial')).toBe(false)
    expect(september.querySelectorAll('rect.mf-cashflow__cursor')).toHaveLength(2)
    expect(september.querySelectorAll('rect:not(.mf-cashflow__cursor)')).toHaveLength(2)
  })

  it('shows a zero In or Out as a dash and a zero Net unsigned in the tooltip', async () => {
    const { column } = setup({ cursor })
    await fireEvent.focusIn(column('Jul 2026'))
    const tip = await screen.findByRole('tooltip')
    const values = [...tip.querySelectorAll('dd')].map((value) => value.textContent)
    expect(values).toEqual(['—', '—', '0.00', '—', '—'])
  })

  it("describes the focused column with the cursor row's amounts", () => {
    const { column } = setup({ cursor })
    const described = column('Sep 2026').getAttribute('aria-describedby')
    expect(described).toBeTruthy()
    expect(document.getElementById(described!)?.textContent).toBe(
      'Online Megastore, in +120.00, out -480.00',
    )
    expect(column('Aug 2026').hasAttribute('aria-describedby')).toBe(false)
  })

  it('describes nothing extra without a cursor row', () => {
    const { column } = setup()
    expect(column('Sep 2026').hasAttribute('aria-describedby')).toBe(false)
  })
})
