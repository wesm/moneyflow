import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import FinanceGrid from './FinanceGrid.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { testAggregateRow, testMoney, testProjection } from '../../test/projection'

const callbacks = () => ({
  onmove: vi.fn(),
  onhome: vi.fn(),
  oncursor: vi.fn(),
  ontoggle: vi.fn(),
  ontoggleall: vi.fn(),
  onactivate: vi.fn(),
  oninformation: vi.fn(),
  onedit: vi.fn(),
  onsort: vi.fn(),
})

const detailView: ViewProjection['view'] = {
  mode: 'detail',
  grouping: 'merchant',
  time_granularity: 'year',
  sort_field: 'date',
  sort_direction: 'desc',
  sort_fields: ['date', 'merchant', 'category', 'account', 'amount'],
}

function grouped(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return testProjection({
    view: {
      mode: 'aggregate',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
      sort_fields: ['merchant', 'count', 'amount'],
    },
    detail_rows: null,
    aggregate_rows: [testAggregateRow()],
    ...overrides,
  })
}

/** Header text; the selection header holds only the "Select all matching" box. */
const headers = () =>
  screen.getAllByRole('columnheader').map((header) => header.textContent?.trim())
const cell = (row: HTMLElement, key: string) =>
  row.querySelector<HTMLElement>(`[role="gridcell"][data-column="${key}"]`)

describe('FinanceGrid', () => {
  afterEach(cleanup)

  it('renders transaction rows and moves through them with keys', async () => {
    const events = callbacks()
    render(FinanceGrid, {
      projection: testProjection({ view: detailView }),
      cursorIndex: 0,
      compact: false,
      ...events,
    })
    const grid = screen.getByRole('grid', { name: 'Financial results' })
    expect(grid.getAttribute('aria-rowcount')).toBe('2')
    const row = screen.getByRole('row', { name: /Example Merchant/ })
    expect(row.getAttribute('aria-selected')).toBe('true')
    expect(row.getAttribute('aria-rowindex')).toBe('2')
    expect(row.id).toBe('moneyflow-row-row-0')
    expect(headers()).toEqual(['', 'Date', 'Merchant', 'Category', 'Account', 'Amount', 'Flags'])
    expect(cell(row, 'amount')?.textContent?.trim()).toBe('-$12.34')
    expect(cell(row, 'merchant')?.textContent?.trim()).toBe('Example Merchant')
    expect(screen.getByRole('columnheader', { name: 'Date' }).getAttribute('aria-sort')).toBe(
      'descending',
    )
    expect(screen.getByRole('columnheader', { name: 'Amount' }).hasAttribute('aria-sort')).toBe(
      false,
    )
    await fireEvent.keyDown(grid, { key: 'j' })
    expect(events.onmove).toHaveBeenCalledWith(1)
    await fireEvent.keyDown(grid, { key: 'Home' })
    expect(events.onhome).toHaveBeenCalled()
    await fireEvent.keyDown(grid, { key: 'End' })
    expect(events.onmove).toHaveBeenCalledTimes(1)
  })

  it('moves the cursor on a row click and toggles selection only from the box', async () => {
    const events = callbacks()
    render(FinanceGrid, {
      projection: grouped({
        total_rows: 2,
        window: { offset: 0, limit: 200, count: 2 },
        aggregate_rows: [
          testAggregateRow(),
          testAggregateRow({ index: 1, identity: 'agg-1', label: 'Other Merchant' }),
        ],
      }),
      cursorIndex: 0,
      compact: false,
      ...events,
    })
    const row = screen.getByRole('row', { name: /Other Merchant/ })
    await fireEvent.click(cell(row, 'net')!)
    expect(events.oncursor).toHaveBeenCalledWith(1)
    expect(events.ontoggle).not.toHaveBeenCalled()
    await fireEvent.click(cell(row, 'select')!)
    expect(events.ontoggle).toHaveBeenCalledWith('agg-1', 'aggregate')
    expect(events.oncursor).toHaveBeenCalledTimes(1)
    await fireEvent.click(screen.getByRole('button', { name: 'Select all matching' }))
    expect(events.ontoggleall).toHaveBeenCalledTimes(1)
    expect(events.oncursor).toHaveBeenCalledTimes(1)
  })

  it('checks the header box only while every matching row is selected', async () => {
    const view = render(FinanceGrid, {
      projection: grouped({ selection_count: 0 }),
      cursorIndex: 0,
      compact: false,
      ...callbacks(),
    })
    const box = () =>
      screen.getByRole('button', { name: 'Select all matching' }).querySelector('.mf-check')!
    expect(box().classList.contains('mf-check--on')).toBe(false)
    await view.rerender({ projection: grouped({ selection_count: 1 }) })
    expect(box().classList.contains('mf-check--on')).toBe(true)
    await view.rerender({
      projection: grouped({
        selection_count: 1,
        total_rows: 2,
        window: { offset: 0, limit: 200, count: 2 },
        aggregate_rows: [testAggregateRow(), testAggregateRow({ index: 1, identity: 'agg-1' })],
      }),
    })
    expect(box().classList.contains('mf-check--on')).toBe(false)
  })

  it('keeps a double-click from selecting text but lets a single press through', async () => {
    render(FinanceGrid, { projection: grouped(), cursorIndex: 0, compact: false, ...callbacks() })
    const name = cell(screen.getByRole('row', { name: /Example Merchant/ }), 'name')!
    expect(await fireEvent.mouseDown(name, { detail: 1 })).toBe(true)
    expect(await fireEvent.mouseDown(name, { detail: 2 })).toBe(false)
  })

  it('double-clicks a grouped row to drill from any cell but the box', async () => {
    const events = callbacks()
    render(FinanceGrid, { projection: grouped(), cursorIndex: 0, compact: false, ...events })
    const row = screen.getByRole('row', { name: /Example Merchant/ })
    await fireEvent.doubleClick(cell(row, 'select')!)
    expect(events.onactivate).not.toHaveBeenCalled()
    await fireEvent.doubleClick(cell(row, 'count')!)
    expect(events.onactivate).toHaveBeenCalledWith('agg-0', 'aggregate')
  })

  it('double-clicks Merchant or Category to edit and elsewhere to open Info', async () => {
    const events = callbacks()
    render(FinanceGrid, {
      projection: testProjection({ view: detailView }),
      cursorIndex: 0,
      compact: false,
      ...events,
    })
    const row = screen.getByRole('row', { name: /Example Merchant/ })
    await fireEvent.doubleClick(cell(row, 'merchant')!)
    expect(events.onedit).toHaveBeenLastCalledWith('row-0', 'merchant')
    await fireEvent.doubleClick(cell(row, 'category')!)
    expect(events.onedit).toHaveBeenLastCalledWith('row-0', 'category')
    expect(events.oninformation).not.toHaveBeenCalled()
    await fireEvent.doubleClick(cell(row, 'date')!)
    expect(events.oninformation).toHaveBeenCalledWith('row-0')
    expect(events.onactivate).not.toHaveBeenCalled()
    expect(events.ontoggle).not.toHaveBeenCalled()
  })

  it('edits Merchant and Category from pencil buttons outside the Tab order', async () => {
    const events = callbacks()
    render(FinanceGrid, {
      projection: testProjection({ view: detailView }),
      cursorIndex: 0,
      compact: false,
      ...events,
    })
    const row = screen.getByRole('row', { name: /Example Merchant/ })
    const merchant = within(cell(row, 'merchant')!).getByRole('button', { name: 'Edit merchant' })
    const category = within(cell(row, 'category')!).getByRole('button', { name: 'Edit category' })
    expect(merchant.getAttribute('tabindex')).toBe('-1')
    expect(category.getAttribute('tabindex')).toBe('-1')
    await fireEvent.click(merchant)
    expect(events.onedit).toHaveBeenLastCalledWith('row-0', 'merchant')
    await fireEvent.click(category)
    expect(events.onedit).toHaveBeenLastCalledWith('row-0', 'category')
    expect(events.oninformation).not.toHaveBeenCalled()
  })

  it('sorts from the server’s sortable headers only', async () => {
    const events = callbacks()
    render(FinanceGrid, { projection: grouped(), cursorIndex: 0, compact: false, ...events })
    await fireEvent.click(screen.getByRole('button', { name: 'Count' }))
    expect(events.onsort).toHaveBeenLastCalledWith('count')
    await fireEvent.click(screen.getByRole('button', { name: 'Net' }))
    expect(events.onsort).toHaveBeenLastCalledWith('amount')
    expect(screen.getByRole('button', { name: 'Count' }).getAttribute('tabindex')).toBe('-1')
    for (const name of ['In', 'Out', 'Share', 'Top category']) {
      expect(screen.queryByRole('button', { name })).toBeNull()
      expect(screen.getByRole('columnheader', { name }).hasAttribute('aria-sort')).toBe(false)
    }
    expect(events.oncursor).not.toHaveBeenCalled()
  })

  it('gives every gridcell its column key and sizes rows from one height', () => {
    render(FinanceGrid, { projection: grouped(), cursorIndex: 0, compact: false, ...callbacks() })
    const grid = screen.getByRole('grid', { name: 'Financial results' })
    expect(grid.style.getPropertyValue('--mf-row-height')).toBe('30px')
    const row = screen.getByRole('row', { name: /Example Merchant/ })
    const keys = within(row)
      .getAllByRole('gridcell')
      .map((gridcell) => gridcell.dataset['column'])
    expect(keys).toEqual(['select', 'name', 'context', 'count', 'in', 'out', 'net', 'share'])
    const headerKeys = screen.getAllByRole('columnheader').map((header) => header.dataset['column'])
    expect(headerKeys).toEqual(keys)
  })

  it('shows the grouped columns and names the sub-grouped dimension', async () => {
    const events = callbacks()
    const view = render(FinanceGrid, {
      projection: grouped({
        aggregate_rows: [
          testAggregateRow({
            top_category: 'Groceries',
            top_category_percent: 82,
            in: testMoney('0', '+0.00'),
          }),
        ],
      }),
      cursorIndex: 0,
      compact: false,
      ...events,
    })
    expect(headers()).toEqual([
      '',
      'Merchant',
      'Top category',
      'Count',
      'In',
      'Out',
      'Net',
      'Share',
    ])
    expect(screen.getByRole('columnheader', { name: 'Net' }).getAttribute('aria-sort')).toBe(
      'descending',
    )
    const row = screen.getByRole('row', { name: /Example Merchant/ })
    expect(cell(row, 'context')?.textContent?.trim()).toBe('Groceries 82%')
    expect(cell(row, 'in')?.textContent?.trim()).toBe('—')
    expect(cell(row, 'out')?.textContent?.trim()).toBe('-5.00')
    expect(cell(row, 'net')?.textContent?.trim()).toBe('-2.00')
    expect(cell(row, 'share')?.textContent?.trim()).toBe('6.2%')

    await view.rerender({
      projection: grouped({
        view: {
          mode: 'detail',
          grouping: 'merchant',
          sub_grouping: 'category',
          time_granularity: 'year',
          sort_field: 'category',
          sort_direction: 'asc',
          sort_fields: ['category', 'count', 'amount'],
        },
        aggregate_rows: [testAggregateRow({ dimension: 'category', group: 'Food' })],
      }),
    })
    expect(headers()[1]).toBe('Category')
    expect(screen.getByRole('columnheader', { name: 'Category' }).getAttribute('aria-sort')).toBe(
      'ascending',
    )
    expect(headers()[2]).toBe('Group')
  })

  it('marks pending rows with a gold dot and dims rows whose transactions are all hidden', () => {
    render(FinanceGrid, {
      projection: grouped({
        aggregate_rows: [
          testAggregateRow({ flags: { selected: false, hidden: true, pending: true } }),
        ],
      }),
      cursorIndex: 0,
      compact: false,
      ...callbacks(),
    })
    const row = screen.getByRole('row', { name: /Example Merchant/ })
    expect(within(cell(row, 'name')!).getByLabelText('Pending edits')).not.toBeNull()
    expect(row.classList.contains('mf-table__row--hidden')).toBe(true)
    expect(row.textContent).not.toContain('pending')
  })

  it('draws the Time columns with a hidden Net bar and the savings rate', () => {
    render(FinanceGrid, {
      projection: grouped({
        view: {
          mode: 'aggregate',
          grouping: 'time',
          time_granularity: 'month',
          sort_field: 'time_period',
          sort_direction: 'asc',
          sort_fields: ['time_period', 'count', 'amount'],
        },
        aggregate_rows: [
          testAggregateRow({
            dimension: 'time',
            label: 'January 2026',
            net_ratio: -5000,
            savings_rate_tenths: -37,
          }),
        ],
      }),
      cursorIndex: 0,
      compact: false,
      ...callbacks(),
    })
    expect(headers()).toEqual([
      '',
      'Period',
      'Count',
      'In',
      'Out',
      'Net',
      'Net bar',
      'Savings rate',
    ])
    expect(screen.getByRole('columnheader', { name: 'Period' }).getAttribute('aria-sort')).toBe(
      'ascending',
    )
    const row = screen.getByRole('row', { name: /January 2026/ })
    const bar = cell(row, 'net_bar')!.firstElementChild as HTMLElement
    expect(bar.getAttribute('aria-hidden')).toBe('true')
    expect(cell(row, 'savings')?.textContent?.trim()).toBe('-3.7%')
  })

  it('draws an Income divider inside the row where Net changes sign', () => {
    render(FinanceGrid, {
      projection: grouped({
        total_rows: 2,
        window: { offset: 0, limit: 200, count: 2 },
        aggregate_rows: [
          testAggregateRow({ total: testMoney('-200', '-2.00') }),
          testAggregateRow({
            index: 1,
            identity: 'agg-1',
            label: 'Example Employer',
            total: testMoney('900', '+9.00'),
          }),
        ],
        view: {
          mode: 'aggregate',
          grouping: 'merchant',
          time_granularity: 'year',
          sort_field: 'amount',
          sort_direction: 'asc',
          sort_fields: ['merchant', 'count', 'amount'],
        },
      }),
      cursorIndex: 0,
      compact: false,
      ...callbacks(),
    })
    expect(screen.getAllByRole('row')).toHaveLength(3)
    const income = screen.getByRole('row', { name: /Example Employer/ })
    const divider = cell(income, 'name')?.querySelector('.mf-table__divider')
    expect(divider?.textContent).toBe('Income')
    expect(income.classList.contains('mf-table__row--divided')).toBe(true)
    expect(divider?.getAttribute('aria-hidden')).toBe('true')
    expect(
      screen.getByRole('row', { name: /Example Merchant/ }).querySelector('.mf-table__divider'),
    ).toBeNull()
  })

  it('opens Info from the Amazon match mark without toggling selection', async () => {
    const events = callbacks()
    const template = testProjection().detail_rows![0]!
    render(FinanceGrid, {
      projection: testProjection({
        view: detailView,
        detail_rows: [
          {
            ...template,
            amazon_match: {
              class: 'exact_order',
              confidence: 'likely',
              first_product: 'Example Product',
              total_matches: 1,
            },
          },
        ],
      }),
      cursorIndex: 0,
      compact: false,
      ...events,
    })
    const mark = screen.getByRole('button', { name: 'Amazon match: Example Product, likely' })
    expect(mark.textContent?.trim()).toBe('~')
    await fireEvent.click(mark)
    expect(events.oninformation).toHaveBeenCalledWith('row-0')
    expect(events.ontoggle).not.toHaveBeenCalled()
  })

  it('shows staged merchant and category values inline and strikes a staged deletion', () => {
    const template = testProjection().detail_rows![0]!
    render(FinanceGrid, {
      projection: testProjection({
        view: detailView,
        total_rows: 2,
        window: { offset: 0, limit: 200, count: 2 },
        detail_rows: [
          {
            ...template,
            flags: { selected: false, hidden: false, pending: true },
            staged: { deleted: false, merchant: 'Renamed Merchant', category: 'Other Category' },
          },
          {
            ...template,
            index: 1,
            identity: 'row-1',
            merchant: 'Deleted Merchant',
            flags: { selected: false, hidden: false, pending: true },
            staged: { deleted: true },
          },
        ],
      }),
      cursorIndex: 0,
      compact: false,
      ...callbacks(),
    })
    const renamed = screen.getByRole('row', { name: /Renamed Merchant/ })
    expect(cell(renamed, 'merchant')?.querySelector('s')?.textContent).toBe('Example Merchant')
    expect(cell(renamed, 'category')?.textContent).toContain('Other Category')
    expect(cell(renamed, 'category')?.querySelector('s')?.textContent).toBe('Example Category')
    expect(renamed.classList.contains('mf-table__row--deleted')).toBe(false)
    const deleted = screen.getByRole('row', { name: /Deleted Merchant/ })
    expect(deleted.classList.contains('mf-table__row--deleted')).toBe(true)
    expect(within(cell(deleted, 'flags')!).getByText('Delete')).not.toBeNull()
  })

  it('shows an empty state without rows', () => {
    render(FinanceGrid, {
      projection: testProjection({
        total_rows: 0,
        detail_rows: [],
        window: { offset: 0, limit: 200, count: 0 },
      }),
      cursorIndex: 0,
      compact: false,
      ...callbacks(),
    })
    expect(screen.getByText('No transactions')).not.toBeNull()
  })

  describe('scrolling through every row of the result', () => {
    afterEach(() => vi.useRealTimers())

    /** One loaded window of the 450-row result, as the server sends it. */
    function loaded(offset: number): ViewProjection {
      const template = testProjection().detail_rows![0]!
      const count = Math.min(200, 450 - offset)
      return testProjection({
        view: detailView,
        total_rows: 450,
        window: { offset, limit: 200, count },
        detail_rows: Array.from({ length: count }, (_, position) => ({
          ...template,
          index: offset + position,
          identity: `row-${offset + position}`,
          flags: { ...template.flags, selected: false },
        })),
      })
    }
    const rowAt = (index: number) =>
      document.querySelector<HTMLElement>(`[role="row"][aria-rowindex="${index + 2}"]`)
    const placeholders = () => [
      ...document.querySelectorAll<HTMLElement>('.mf-table__row--placeholder'),
    ]

    it('holds the whole result and draws placeholders for rows no loaded window has', () => {
      // Rows 195 to 214 fill the viewport below the header.
      render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 195,
        compact: false,
        scrollTop: 195 * 30,
        viewport: 630,
        ...callbacks(),
      })
      expect(rowAt(199)?.id).toBe('moneyflow-row-row-199')
      expect(rowAt(200)).toBeNull()
      const blanks = placeholders()
      expect(blanks.length).toBeGreaterThan(0)
      for (const blank of blanks) {
        expect(blank.getAttribute('aria-hidden')).toBe('true')
        expect(Number(blank.dataset['index'])).toBeGreaterThanOrEqual(200)
      }
      const rendered = document.querySelectorAll('.mf-table__row').length
      expect(rendered).toBeLessThan(40)
      const spacers = [...document.querySelectorAll<HTMLElement>('.mf-table__spacer')]
      const spacerHeight = spacers.reduce((sum, spacer) => sum + parseFloat(spacer.style.height), 0)
      expect(spacerHeight + rendered * 30).toBe(450 * 30)
    })

    it('draws rows from a cached neighboring window instead of placeholders', () => {
      render(FinanceGrid, {
        projection: loaded(0),
        neighbors: [loaded(200)],
        cursorIndex: 195,
        compact: false,
        scrollTop: 195 * 30,
        viewport: 630,
        ...callbacks(),
      })
      expect(rowAt(200)?.id).toBe('moneyflow-row-row-200')
      expect(placeholders()).toEqual([])
    })

    it('asks once per frame for the window holding the first fully visible row', async () => {
      vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame'] })
      const events = { ...callbacks(), onshow: vi.fn() }
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 0,
        compact: false,
        scrollTop: 0,
        viewport: 630,
        ...events,
      })
      await view.rerender({ scrollTop: 150 * 30 })
      vi.advanceTimersToNextFrame()
      expect(events.onshow).not.toHaveBeenCalled()

      await view.rerender({ scrollTop: 280 * 30 + 10 })
      await view.rerender({ scrollTop: 300 * 30 + 10 })
      expect(events.onshow).not.toHaveBeenCalled()
      vi.advanceTimersToNextFrame()
      expect(events.onshow).toHaveBeenCalledTimes(1)
      expect(events.onshow).toHaveBeenCalledWith(301)
    })

    it('leaves the scroll position alone when a scrolled-to window moves the cursor', async () => {
      vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame'] })
      const events = { ...callbacks(), onshow: vi.fn() }
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 0,
        compact: false,
        scrollTop: 0,
        viewport: 630,
        ...events,
      })
      await view.rerender({ scrollTop: 300 * 30 })
      vi.advanceTimersToNextFrame()
      expect(events.onshow).toHaveBeenCalledWith(300)
      // The user scrolls on before the window arrives, which then puts the cursor on row 300.
      await view.rerender({ scrollTop: 310 * 30 })
      await view.rerender({ projection: loaded(200), cursorIndex: 300 })

      expect(rowAt(330)).not.toBeNull()
      expect(rowAt(300)).toBeNull()
      // The cursor moves on to the row now first on screen, without scrolling back.
      expect(events.oncursor).toHaveBeenCalledWith(310)
    })

    it('stays in the window a key moved the cursor into at its bottom edge', async () => {
      vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame'] })
      const events = { ...callbacks(), onshow: vi.fn() }
      // Rows 180 to 199 show, with the cursor on the last of them.
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 199,
        compact: false,
        scrollTop: 180 * 30,
        viewport: 630,
        ...events,
      })
      const grid = screen.getByRole('grid', { name: 'Financial results' })
      await fireEvent.keyDown(grid, { key: 'j' })
      expect(events.onmove).toHaveBeenCalledWith(1)
      await view.rerender({ projection: loaded(200), cursorIndex: 200 })
      vi.advanceTimersToNextFrame()

      expect(rowAt(200)).not.toBeNull()
      expect(events.onshow).not.toHaveBeenCalled()
    })

    it('keeps the cursor on a clicked row from a cached neighboring window', async () => {
      vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame'] })
      const events = { ...callbacks(), onshow: vi.fn() }
      const view = render(FinanceGrid, {
        projection: loaded(0),
        neighbors: [loaded(200)],
        cursorIndex: 195,
        compact: false,
        scrollTop: 195 * 30,
        viewport: 630,
        ...events,
      })
      await fireEvent.click(cell(rowAt(205)!, 'date')!)
      expect(events.oncursor).toHaveBeenCalledWith(205)
      await view.rerender({ projection: loaded(200), neighbors: [loaded(0)], cursorIndex: 205 })
      vi.advanceTimersToNextFrame()

      expect(events.onshow).not.toHaveBeenCalled()
      expect(events.oncursor).toHaveBeenCalledTimes(1)
    })
  })

  it('keeps the active descendant inside the rendered rows as the cursor moves', async () => {
    const template = testProjection().detail_rows![0]!
    const detail_rows = Array.from({ length: 100 }, (_, index) => ({
      ...template,
      index,
      identity: `row-${index}`,
      flags: { ...template.flags, selected: false },
    }))
    const projection = testProjection({
      view: detailView,
      total_rows: 100,
      detail_rows,
      window: { offset: 0, limit: 200, count: 100 },
    })
    const view = render(FinanceGrid, {
      projection,
      cursorIndex: 60,
      compact: false,
      ...callbacks(),
    })
    const grid = screen.getByRole('grid', { name: 'Financial results' })
    const activeElement = () => document.getElementById(grid.getAttribute('aria-activedescendant')!)
    expect(grid.getAttribute('aria-activedescendant')).toBe('moneyflow-row-row-60')
    expect(activeElement()).not.toBeNull()

    await view.rerender({ cursorIndex: 2 })
    expect(grid.getAttribute('aria-activedescendant')).toBe('moneyflow-row-row-2')
    expect(activeElement()).not.toBeNull()

    await view.rerender({ cursorIndex: 99 })
    expect(grid.getAttribute('aria-activedescendant')).toBe('moneyflow-row-row-99')
    expect(activeElement()).not.toBeNull()
  })
})
