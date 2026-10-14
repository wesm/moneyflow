import { describe, expect, it } from 'vitest'

import {
  activeCurrency,
  anchorDate,
  calendarStep,
  canShiftPeriod,
  chooserTab,
  customDraft,
  emptyScope,
  gridStep,
  monthCells,
  periodContaining,
  periodLabel,
  scopeKind,
  yearCells,
} from './period'
import { testProjection } from '../../test/projection'
import type { ViewProjection } from '../api/client'

describe('scopeKind', () => {
  it.each([
    [undefined, 'all'],
    [{ from: '2026-09-15', to: '2026-09-15' }, 'day'],
    [{ from: '2026-09-01', to: '2026-09-30' }, 'month'],
    [{ from: '2024-02-01', to: '2024-02-29' }, 'month'],
    [{ from: '2026-01-01', to: '2026-12-31' }, 'year'],
    [{ from: '2026-03-10', to: '2026-04-09' }, 'custom'],
    [{ from: '2026-09-01', to: '2026-09-29' }, 'custom'],
    [{ from: '2026-01-01', to: '2026-12-30' }, 'custom'],
    [{ from: '2025-01-01', to: '2026-12-31' }, 'custom'],
  ] as const)('classifies %o as %s', (range, kind) => {
    expect(scopeKind(range)).toBe(kind)
  })
})

describe('periodLabel', () => {
  it.each([
    [{ from: '2026-09-15', to: '2026-09-15' }, 'Sep 15, 2026'],
    [{ from: '2026-09-01', to: '2026-09-30' }, 'Sep 2026'],
    [{ from: '2026-01-01', to: '2026-12-31' }, '2026'],
    [{ from: '2026-03-10', to: '2026-04-09' }, 'Mar 10 – Apr 9, 2026'],
    [{ from: '2025-12-20', to: '2026-01-05' }, 'Dec 20, 2025 – Jan 5, 2026'],
    [undefined, 'All time'],
  ] as const)('labels %o as %s', (range, label) => {
    expect(periodLabel(range)).toBe(label)
  })
})

describe('anchorDate', () => {
  const latest = '2026-10-07'

  it('clamps to Latest when the scope contains it', () => {
    expect(anchorDate({ from: '2026-10-01', to: '2026-10-31' }, latest)).toBe('2026-10-07')
    expect(anchorDate({ from: '2026-01-01', to: '2026-12-31' }, latest)).toBe('2026-10-07')
  })

  it('uses the last day of a scope before Latest', () => {
    expect(anchorDate({ from: '2026-09-01', to: '2026-09-30' }, latest)).toBe('2026-09-30')
    expect(anchorDate({ from: '2026-03-10', to: '2026-04-09' }, latest)).toBe('2026-04-09')
  })

  it('uses the last day of a scope after Latest', () => {
    expect(anchorDate({ from: '2026-11-01', to: '2026-11-30' }, latest)).toBe('2026-11-30')
  })

  it('uses Latest for All', () => {
    expect(anchorDate(undefined, latest)).toBe('2026-10-07')
  })

  it('has no anchor when the currency has no transactions', () => {
    expect(anchorDate(undefined, undefined)).toBeUndefined()
    expect(anchorDate({ from: '2026-09-01', to: '2026-09-30' }, undefined)).toBeUndefined()
  })
})

describe('periodContaining', () => {
  it.each([
    ['2026-09-15', 'day', '2026-09-15', '2026-09-15'],
    ['2026-01-31', 'month', '2026-01-01', '2026-01-31'],
    ['2026-04-30', 'month', '2026-04-01', '2026-04-30'],
    ['2026-12-31', 'month', '2026-12-01', '2026-12-31'],
    ['2026-02-14', 'month', '2026-02-01', '2026-02-28'],
    ['2024-02-29', 'month', '2024-02-01', '2024-02-29'],
    ['2024-02-29', 'day', '2024-02-29', '2024-02-29'],
    ['2024-02-29', 'year', '2024-01-01', '2024-12-31'],
    ['2025-12-31', 'year', '2025-01-01', '2025-12-31'],
    ['2026-01-01', 'year', '2026-01-01', '2026-12-31'],
  ] as const)('%s in a %s is %s to %s', (date, kind, start, end) => {
    expect(periodContaining(date, kind)).toEqual({ start, end })
  })
})

describe('gridStep', () => {
  const none = [false, false, false, false, false, false]

  it('moves by one across a row and by a row up and down', () => {
    expect(gridStep('ArrowRight', 1, none, 3)).toBe(2)
    expect(gridStep('ArrowLeft', 1, none, 3)).toBe(0)
    expect(gridStep('ArrowDown', 1, none, 3)).toBe(4)
    expect(gridStep('ArrowUp', 4, none, 3)).toBe(1)
  })

  it('stays inside the grid', () => {
    expect(gridStep('ArrowLeft', 0, none, 3)).toBe(0)
    expect(gridStep('ArrowRight', 5, none, 3)).toBe(5)
    expect(gridStep('ArrowUp', 1, none, 3)).toBe(1)
    expect(gridStep('ArrowDown', 4, none, 3)).toBe(4)
  })

  it('skips disabled cells and goes to the ends with Home and End', () => {
    const disabled = [true, false, true, false, false, true]
    expect(gridStep('ArrowRight', 1, disabled, 3)).toBe(3)
    expect(gridStep('ArrowLeft', 1, disabled, 3)).toBe(1)
    expect(gridStep('Home', 4, disabled, 3)).toBe(1)
    expect(gridStep('End', 1, disabled, 3)).toBe(4)
  })

  it('ignores other keys', () => {
    expect(gridStep('Enter', 1, none, 3)).toBeUndefined()
  })
})

describe('calendarStep', () => {
  const limits = { first: '2024-02-10', latest: '2026-10-07' }

  it('moves a day with Left and Right and a week with Up and Down', () => {
    expect(calendarStep('ArrowRight', '2026-09-15', '2026-09-01', limits)).toEqual({
      date: '2026-09-16',
      month: '2026-09-01',
    })
    expect(calendarStep('ArrowLeft', '2026-09-15', '2026-09-01', limits)?.date).toBe('2026-09-14')
    expect(calendarStep('ArrowDown', '2026-09-15', '2026-09-01', limits)?.date).toBe('2026-09-22')
    expect(calendarStep('ArrowUp', '2026-09-15', '2026-09-01', limits)?.date).toBe('2026-09-08')
  })

  it('pages to the new month when a step leaves the shown month', () => {
    expect(calendarStep('ArrowRight', '2026-09-30', '2026-09-01', limits)).toEqual({
      date: '2026-10-01',
      month: '2026-10-01',
    })
    expect(calendarStep('ArrowUp', '2026-10-03', '2026-10-15', limits)).toEqual({
      date: '2026-09-26',
      month: '2026-09-26',
    })
    expect(calendarStep('ArrowLeft', '2026-01-01', '2026-01-01', limits)).toEqual({
      date: '2025-12-31',
      month: '2025-12-31',
    })
  })

  it('pages from a day shown outside its month back to that month', () => {
    expect(calendarStep('ArrowLeft', '2026-08-31', '2026-09-10', limits)).toEqual({
      date: '2026-08-30',
      month: '2026-08-30',
    })
    expect(calendarStep('ArrowRight', '2026-08-31', '2026-09-10', limits)).toEqual({
      date: '2026-09-01',
      month: '2026-09-10',
    })
  })

  it('counts leap days', () => {
    const wide = { first: '2020-01-01', latest: '2030-12-31' }
    expect(calendarStep('ArrowRight', '2028-02-28', '2028-02-01', wide)?.date).toBe('2028-02-29')
    expect(calendarStep('ArrowRight', '2027-02-28', '2027-02-01', wide)?.date).toBe('2027-03-01')
    expect(calendarStep('ArrowDown', '2028-02-25', '2028-02-01', wide)?.date).toBe('2028-03-03')
  })

  it('stays put instead of stepping past the latest or before the first day', () => {
    const latest = { date: '2026-10-07', month: '2026-10-01' }
    expect(calendarStep('ArrowRight', '2026-10-07', '2026-10-01', limits)).toEqual(latest)
    const nearLatest = { date: '2026-10-03', month: '2026-10-01' }
    expect(calendarStep('ArrowDown', '2026-10-03', '2026-10-01', limits)).toEqual(nearLatest)
    const first = { date: '2024-02-10', month: '2024-02-01' }
    expect(calendarStep('ArrowLeft', '2024-02-10', '2024-02-01', limits)).toEqual(first)
    const nearFirst = { date: '2024-02-14', month: '2024-02-01' }
    expect(calendarStep('ArrowUp', '2024-02-14', '2024-02-01', limits)).toEqual(nearFirst)
  })

  it('still moves toward the enabled days from a day before the first', () => {
    expect(calendarStep('ArrowRight', '2024-02-03', '2024-02-01', limits)?.date).toBe('2024-02-04')
    expect(calendarStep('ArrowLeft', '2024-02-03', '2024-02-01', limits)?.date).toBe('2024-02-03')
  })

  it('ignores other keys', () => {
    expect(calendarStep('Enter', '2026-09-15', '2026-09-01', limits)).toBeUndefined()
    expect(calendarStep('Home', '2026-09-15', '2026-09-01', limits)).toBeUndefined()
  })
})

describe('activeCurrency', () => {
  const usd = { currency: 'USD', scale: 2, count: 3, first: '2024-01-01', latest: '2026-10-07' }
  const eur = { currency: 'EUR', scale: 2, count: 1, first: '2025-05-01', latest: '2025-05-01' }
  const view = testProjection().view

  it('finds the view currency with its dates', () => {
    const projection = testProjection({
      currencies: [usd, eur],
      view: { ...view, currency: { currency: 'EUR', scale: 2 } },
    })
    expect(activeCurrency(projection)).toEqual(eur)
  })

  it('has none before the view sets a currency', () => {
    expect(activeCurrency(testProjection({ currencies: [usd] }))).toBeUndefined()
  })
})

describe('canShiftPeriod', () => {
  const filters = (from: string, to: string) => ({
    show_hidden: false,
    show_transfers: false,
    date_range: { from, to },
  })

  it('shifts one day, month, or year', () => {
    for (const [from, to] of [
      ['2026-09-15', '2026-09-15'],
      ['2026-09-01', '2026-09-30'],
      ['2026-01-01', '2026-12-31'],
    ] as const) {
      expect(canShiftPeriod(testProjection({ filters: filters(from, to) }))).toBe(true)
    }
  })

  it('does not shift a custom range or all time without a time drill', () => {
    expect(canShiftPeriod(testProjection({ filters: filters('2026-03-10', '2026-04-09') }))).toBe(
      false,
    )
    expect(canShiftPeriod(testProjection())).toBe(false)
  })

  it('shifts a time drill under all time, the only drill-down included', () => {
    const drilled = { ...testProjection().view, drill_depth: 1 }
    const time = [{ dimension: 'time', label: 'Sep 2026' }]
    expect(canShiftPeriod(testProjection({ view: drilled, breadcrumbs: time }))).toBe(true)
    const corner = [{ dimension: 'merchant', label: 'Corner Grocery' }]
    expect(canShiftPeriod(testProjection({ view: drilled, breadcrumbs: corner }))).toBe(false)
  })

  it('does not count the top-level Time crumb as a drill', () => {
    const time = [{ dimension: 'time', label: 'Time' }]
    expect(canShiftPeriod(testProjection({ breadcrumbs: time }))).toBe(false)
  })
})

describe('chooser', () => {
  it('opens on the scope kind, and on Month for All and Custom', () => {
    expect(chooserTab('day')).toBe('day')
    expect(chooserTab('year')).toBe('year')
    expect(chooserTab('all')).toBe('month')
    expect(chooserTab('custom')).toBe('month')
  })

  it('starts the Custom draft from a custom range only', () => {
    expect(customDraft({ from: '2026-03-10', to: '2026-04-09' })).toEqual({
      from: '2026-03-10',
      to: '2026-04-09',
    })
    expect(customDraft({ from: '2026-09-01', to: '2026-09-30' })).toEqual({ from: '', to: '' })
    expect(customDraft(undefined)).toEqual({ from: '', to: '' })
  })

  it('enables months from the first transaction to Latest and marks the scope month', () => {
    const cells = monthCells(
      2024,
      { from: '2024-02-01', to: '2024-02-29' },
      '2024-02-10',
      '2024-11-03',
    )
    expect(cells.map((cell) => cell.disabled)).toEqual([
      true,
      false,
      false,
      false,
      false,
      false,
      false,
      false,
      false,
      false,
      false,
      true,
    ])
    expect(cells.filter((cell) => cell.selected).map((cell) => cell.name)).toEqual(['Feb 2024'])
    expect(cells[1]).toMatchObject({ key: '2024-02-01', label: 'Feb' })
  })

  it('lists one year from the first transaction to Latest and marks the scope year', () => {
    const cells = yearCells(2024, 2026, { from: '2025-01-01', to: '2025-12-31' })
    expect(cells.map((cell) => cell.key)).toEqual(['2024-01-01', '2025-01-01', '2026-01-01'])
    expect(cells.map((cell) => cell.selected)).toEqual([false, true, false])
    expect(
      yearCells(2024, 2026, { from: '2025-03-01', to: '2025-03-31' }).some((c) => c.selected),
    ).toBe(false)
  })
})

describe('emptyScope', () => {
  const usd = { currency: 'USD', scale: 2, count: 3, first: '2024-01-01', latest: '2026-10-07' }
  const money = { minor: '0', currency: 'USD', scale: 2, decimal: '0.00', display: '0.00' }
  const stats = (count: number) => ({
    currency: 'USD',
    scale: 2,
    count,
    in: money,
    out: money,
    net: money,
  })
  const base = testProjection()
  function scoped(from: string, to: string, overrides: Partial<ViewProjection> = {}) {
    return testProjection({
      currencies: [usd],
      view: { ...base.view, currency: { currency: 'USD', scale: 2 } },
      filters: { show_hidden: false, show_transfers: false, date_range: { from, to } },
      statistics: [],
      ...overrides,
    })
  }

  it('offers the period at the same resolution that contains Latest', () => {
    expect(emptyScope(scoped('2026-11-01', '2026-11-30'))).toEqual({
      label: 'Nov 2026',
      jump: { start: '2026-10-01', end: '2026-10-31' },
    })
    expect(emptyScope(scoped('2026-11-15', '2026-11-15'))?.jump).toEqual({
      start: '2026-10-07',
      end: '2026-10-07',
    })
    expect(emptyScope(scoped('2027-01-01', '2027-12-31'))?.jump).toEqual({
      start: '2026-01-01',
      end: '2026-12-31',
    })
  })

  it('jumps from an empty custom range to the month containing Latest', () => {
    expect(emptyScope(scoped('2023-03-10', '2023-04-09'))).toEqual({
      label: 'Mar 10 – Apr 9, 2023',
      jump: { start: '2026-10-01', end: '2026-10-31' },
    })
  })

  it('is not an empty period once the period has transactions', () => {
    expect(emptyScope(scoped('2026-09-01', '2026-09-30', { statistics: [stats(4)] }))).toBe(
      undefined,
    )
  })

  it('offers no jump to the period already shown', () => {
    expect(emptyScope(scoped('2026-10-01', '2026-10-31'))).toBeUndefined()
    expect(emptyScope(scoped('2026-10-07', '2026-10-07'))).toBeUndefined()
  })

  it('is not an empty period for all time, a search, or a view without a currency', () => {
    const allTime = testProjection({
      currencies: [usd],
      view: { ...base.view, currency: { currency: 'USD', scale: 2 } },
      statistics: [],
    })
    expect(emptyScope(allTime)).toBeUndefined()
    const searched = scoped('2026-09-01', '2026-09-30')
    searched.view.search = 'no such merchant'
    expect(emptyScope(searched)).toBeUndefined()
    expect(
      emptyScope(scoped('2026-11-01', '2026-11-30', { view: base.view, currencies: [usd] })),
    ).toBeUndefined()
  })

  it('leaves every empty drill-down to the table', () => {
    const view = { ...base.view, currency: { currency: 'USD', scale: 2 }, drill_depth: 1 }
    const corner = [{ dimension: 'merchant', label: 'Corner Grocery' }]
    expect(
      emptyScope(scoped('2026-11-01', '2026-11-30', { view, breadcrumbs: corner })),
    ).toBeUndefined()
    const november = [{ dimension: 'time', label: 'Nov 2026' }]
    expect(
      emptyScope(scoped('2026-11-01', '2026-11-30', { view, breadcrumbs: november })),
    ).toBeUndefined()
  })
})
