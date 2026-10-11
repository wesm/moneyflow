import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest'

import FinanceGrid from './FinanceGrid.svelte'
import { status } from '../../lib/shell/status.svelte'
import type { InsightsSeries, ViewProjection } from '../../lib/api/client'
import type { RowInsights } from '../../lib/table/trend'
import { testInsights, testSeries } from '../../test/insights'
import { testAggregateRow, testMoney, testProjection } from '../../test/projection'

const callbacks = () => ({
  onmove: vi.fn(),
  onhome: vi.fn(),
  oncursor: vi.fn(() => Promise.resolve()),
  ontoggle: vi.fn(),
  ontoggleall: vi.fn(),
  onactivate: vi.fn(),
  oninformation: vi.fn(),
  onedit: vi.fn(),
  onsort: vi.fn(),
  onactions: vi.fn(),
})

const detailView: ViewProjection['view'] = {
  mode: 'detail',
  grouping: 'merchant',
  time_granularity: 'year',
  sort_field: 'date',
  sort_direction: 'desc',
  sort_fields: ['date', 'merchant', 'category', 'account', 'amount'],
}

/** A Sep 2026 scope, which has an August comparison. */
const septemberFilters: ViewProjection['filters'] = {
  show_hidden: false,
  show_transfers: false,
  date_range: { from: '2026-09-01', to: '2026-09-30' },
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

const september = grouped({ filters: septemberFilters })

/** Header text; the selection header holds only the "Select all matching" box. */
const headers = () =>
  screen.getAllByRole('columnheader').map((header) => header.textContent?.trim())
const cell = (row: HTMLElement, key: string) =>
  row.querySelector<HTMLElement>(`[role="gridcell"][data-column="${key}"]`)

/** The window is 760 px wide while `wide` is true, else 700 px. */
let wide = true
const windowWidth = Object.getOwnPropertyDescriptor(window, 'innerWidth')!
beforeEach(() => {
  wide = true
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    get: () => (wide ? 760 : 700),
  })
})
afterEach(() => Object.defineProperty(window, 'innerWidth', windowWidth))

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
    expect(keys).toEqual([
      'select',
      'name',
      'context',
      'count',
      'in',
      'out',
      'net',
      'share',
      'trend',
    ])
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
      'Trend',
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

  describe('compact rows at 640 px and narrower', () => {
    const texts = (row: HTMLElement) =>
      within(row)
        .getAllByRole('gridcell')
        .map((gridcell) => [gridcell.dataset['column'], gridcell.textContent?.trim()])

    it('draws a grouped row on two lines with a 52 px height and a drill button', () => {
      render(FinanceGrid, { projection: grouped(), cursorIndex: 0, compact: true, ...callbacks() })
      const grid = screen.getByRole('grid', { name: 'Financial results' })
      expect(grid.style.getPropertyValue('--mf-row-height')).toBe('52px')
      const row = screen.getByRole('row', { name: /Example Merchant/ })
      expect(texts(row)).toEqual([
        ['select', ''],
        ['name', 'Example Merchant'],
        ['net', '-2.00'],
        ['count', '2 txns'],
        ['in', 'In +3.00'],
        ['out', 'Out -5.00'],
        ['actions', ''],
      ])
      const open = within(cell(row, 'actions')!).getByRole('button', {
        name: 'Open Example Merchant',
      })
      expect(open.getAttribute('tabindex')).toBe('-1')
    })

    it('shows only the select-all control and the current sort field in the header', () => {
      render(FinanceGrid, { projection: grouped(), cursorIndex: 0, compact: true, ...callbacks() })
      const keys = screen.getAllByRole('columnheader').map((header) => header.dataset['column'])
      expect(keys).toEqual(['select', 'net'])
      expect(screen.getByRole('button', { name: 'Select all matching' })).not.toBeNull()
      expect(screen.getByRole('columnheader', { name: 'Net' }).getAttribute('aria-sort')).toBe(
        'descending',
      )
    })

    it('toggles from the tap area and drills from the row body or its button', async () => {
      const events = callbacks()
      render(FinanceGrid, { projection: grouped(), cursorIndex: 0, compact: true, ...events })
      const row = screen.getByRole('row', { name: /Example Merchant/ })
      await fireEvent.click(cell(row, 'select')!)
      expect(events.ontoggle).toHaveBeenCalledWith('agg-0', 'aggregate')
      expect(events.onactivate).not.toHaveBeenCalled()
      await fireEvent.click(cell(row, 'out')!)
      expect(events.onactivate).toHaveBeenCalledWith('agg-0', 'aggregate')
      await fireEvent.click(screen.getByRole('button', { name: 'Open Example Merchant' }))
      expect(events.onactivate).toHaveBeenCalledTimes(2)
      await fireEvent.doubleClick(cell(row, 'name')!)
      expect(events.onactivate).toHaveBeenCalledTimes(2)
      expect(events.ontoggle).toHaveBeenCalledTimes(1)
    })

    it('draws a transaction row on two lines and opens Info or the actions sheet', async () => {
      const events = callbacks()
      render(FinanceGrid, {
        projection: testProjection({ view: detailView }),
        cursorIndex: 0,
        compact: true,
        ...events,
      })
      const row = screen.getByRole('row', { name: /Example Merchant/ })
      expect(texts(row)).toEqual([
        ['select', ''],
        ['merchant', 'Example Merchant'],
        ['amount', '-$12.34'],
        ['date', '2024-01-02'],
        ['category', 'Example Category'],
        ['account', 'Account Name'],
        ['flags', ''],
        ['actions', ''],
      ])
      await fireEvent.click(cell(row, 'category')!)
      expect(events.oncursor).toHaveBeenLastCalledWith(0)
      expect(events.oninformation).toHaveBeenCalledWith('row-0')
      expect(events.onedit).not.toHaveBeenCalled()
      const actions = screen.getByRole('button', { name: 'Actions for Example Merchant' })
      expect(actions.getAttribute('aria-haspopup')).toBe('dialog')
      await fireEvent.click(actions)
      expect(events.onactions).toHaveBeenCalledWith(
        expect.objectContaining({ identity: 'row-0' }),
        actions,
      )
      expect(events.oninformation).toHaveBeenCalledTimes(1)
    })
  })

  describe('compact rows: headers, taps, and keys', () => {
    /** Each gridcell's column key with its `aria-colindex`. */
    const columnIndexes = (element: HTMLElement, role: string) =>
      Object.fromEntries(
        within(element)
          .getAllByRole(role)
          .map((node) => [node.dataset['column'], node.getAttribute('aria-colindex')]),
      )

    it('numbers the compact cells so each value has its own header', () => {
      for (const [projection, sorted] of [
        [grouped(), 'net'],
        [testProjection({ view: detailView }), 'date'],
      ] as const) {
        render(FinanceGrid, { projection, cursorIndex: 0, compact: true, ...callbacks() })
        const grid = screen.getByRole('grid', { name: 'Financial results' })
        const row = screen.getByRole('row', { name: /Example Merchant/ })
        const cells = columnIndexes(row, 'gridcell')
        const headers = columnIndexes(grid.querySelector('.mf-table__header')!, 'columnheader')
        expect(headers).toEqual({ select: '1', [sorted]: cells[sorted] })
        const indexes = Object.values(cells).map(Number)
        expect(indexes).toEqual(indexes.map((_, position) => position + 1))
        expect(grid.getAttribute('aria-colcount')).toBe(String(indexes.length))
        cleanup()
      }
    })

    it('leaves desktop cells in document order without column numbers', () => {
      render(FinanceGrid, { projection: grouped(), cursorIndex: 0, compact: false, ...callbacks() })
      const grid = screen.getByRole('grid', { name: 'Financial results' })
      expect(grid.querySelector('[aria-colindex]')).toBeNull()
      expect(grid.hasAttribute('aria-colcount')).toBe(false)
    })

    it('moves the cursor to a tapped group before drilling, so Back returns to it', async () => {
      const events = callbacks()
      const rows = [testAggregateRow(), testAggregateRow({ index: 1, identity: 'agg-1' })]
      render(FinanceGrid, {
        projection: grouped({
          aggregate_rows: rows,
          total_rows: 2,
          window: { offset: 0, limit: 200, count: 2 },
        }),
        cursorIndex: 0,
        compact: true,
        ...events,
      })
      const second = document.querySelector<HTMLElement>('[role="row"][aria-rowindex="3"]')!
      await fireEvent.click(cell(second, 'net')!)
      await vi.waitFor(() => expect(events.onactivate).toHaveBeenCalledWith('agg-1', 'aggregate'))
      expect(events.oncursor).toHaveBeenCalledWith(1)
      const [moved] = events.oncursor.mock.invocationCallOrder
      const [drilled] = events.onactivate.mock.invocationCallOrder
      expect(moved).toBeLessThan(drilled!)
    })

    it('reports a tap whose cursor move failed, and does not drill', async () => {
      onTestFinished(() => status.clear())
      const events = callbacks()
      events.oncursor.mockImplementation(() => Promise.reject(new Error('offline')))
      render(FinanceGrid, { projection: grouped(), cursorIndex: 0, compact: true, ...events })
      const row = document.querySelector<HTMLElement>('[role="row"][aria-rowindex="2"]')!
      await fireEvent.click(cell(row, 'net')!)
      await vi.waitFor(() =>
        expect(status.notice).toBe('The cursor could not move to that row. Try again.'),
      )
      expect(events.onactivate).not.toHaveBeenCalled()
    })

    it('takes focus back from a row button before a grid key moves the cursor', async () => {
      const events = callbacks()
      render(FinanceGrid, {
        projection: testProjection({ view: detailView }),
        cursorIndex: 0,
        compact: true,
        ...events,
      })
      const grid = screen.getByRole('grid', { name: 'Financial results' })
      const more = screen.getByRole('button', { name: 'Actions for Example Merchant' })
      more.focus()
      await fireEvent.keyDown(more, { key: 'j' })
      expect(events.onmove).toHaveBeenCalledWith(1)
      expect(document.activeElement).toBe(grid)
    })
  })

  it('draws Trend and vs prior from the row series and names the comparison', async () => {
    const loaded = new Map<string, InsightsSeries>()
    const insights = (): RowInsights => ({
      response: testInsights(),
      failed: false,
      seriesFor: (identity) => loaded.get(identity),
    })
    const view = render(FinanceGrid, {
      projection: september,
      insights: insights(),
      cursorIndex: 0,
      compact: false,
      ...callbacks(),
    })
    expect(headers().slice(-2)).toEqual(['Trend', 'vs Aug'])
    const row = screen.getByRole('row', { name: /Example Merchant/ })
    /** The row's accessible name, as a screen reader announces it. */
    const spoken = () => {
      let name = ''
      screen.getByRole('row', {
        name: (accessible, element) => (element === row ? ((name = accessible), true) : false),
      })
      return name
    }
    const before = spoken()
    expect(cell(row, 'trend')?.querySelector('.mf-skeleton')).not.toBeNull()
    expect(cell(row, 'vs_prior')?.querySelector('.mf-skeleton')).not.toBeNull()

    loaded.set('agg-0', {
      ...testSeries('agg-0', '-30000'),
      vs_prior: { kind: 'percent', percent_tenths: 200, grew: true, favorable: false },
    })
    await view.rerender({ insights: insights() })
    expect(cell(row, 'trend')?.querySelector('svg')?.getAttribute('aria-hidden')).toBe('true')
    expect(cell(row, 'vs_prior')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
      '▲ 20.0% up 20.0% vs Aug',
    )
    // The sparkline and the glyphs add nothing to what the row says; vs prior adds its words.
    expect(spoken()).toBe(`${before} up 20.0% vs Aug`)
  })

  const columnKeys = () =>
    screen.getAllByRole('columnheader').map((header) => header.dataset['column'])

  it('drops Context, Trend, and vs prior below 760 px', () => {
    render(FinanceGrid, { projection: september, cursorIndex: 0, compact: false, ...callbacks() })
    expect(columnKeys()).toContain('vs_prior')
    cleanup()
    wide = false
    render(FinanceGrid, { projection: september, cursorIndex: 0, compact: false, ...callbacks() })
    expect(columnKeys()).toEqual(['select', 'name', 'count', 'in', 'out', 'net', 'share'])
  })

  it('drops the columns that overflow the scroller instead of scrolling, below 900 px too', () => {
    const scroller = document.createElement('div')
    // 56 rem at a 16 px root: Context drops, Trend and vs prior still fit.
    Object.defineProperty(scroller, 'clientWidth', { configurable: true, value: 56 * 16 })
    const rootFontSize = document.documentElement.style.fontSize
    onTestFinished(() => {
      document.documentElement.style.fontSize = rootFontSize
    })
    document.documentElement.style.fontSize = '16px'
    const props = { projection: september, cursorIndex: 0, compact: false, scroller }
    render(FinanceGrid, { ...props, ...callbacks() })
    const keys = ['select', 'name', 'count', 'in', 'out', 'net', 'share']
    expect(columnKeys()).toEqual([...keys, 'trend', 'vs_prior'])
    const row = screen.getByRole('row', { name: /Example Merchant/ })
    expect(cell(row, 'context')).toBeNull()
    cleanup()
    Object.defineProperty(scroller, 'clientWidth', { configurable: true, value: 50 * 16 })
    render(FinanceGrid, { ...props, ...callbacks() })
    expect(columnKeys()).toEqual(keys)
  })

  describe('scrolling through every row of the result', () => {
    afterEach(() => vi.useRealTimers())

    /** A scroller element whose `scrollTop` keeps what the grid writes, as jsdom's does not. */
    function scrollerAt(scrollTop: number): HTMLElement {
      const scroller = document.createElement('div')
      Object.defineProperty(scroller, 'scrollTop', { value: scrollTop, writable: true })
      return scroller
    }

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

    it('asks for the next window when the user scrolled past the arriving one', async () => {
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
      expect(events.onshow).toHaveBeenLastCalledWith(300)
      // The user scrolls past the window on its way before it arrives.
      await view.rerender({ scrollTop: 410 * 30 })
      await view.rerender({ projection: loaded(200), cursorIndex: 300 })
      vi.advanceTimersToNextFrame()

      expect(events.oncursor).not.toHaveBeenCalled()
      expect(events.onshow).toHaveBeenLastCalledWith(410)
    })

    it('leaves the cursor alone when the arriving window shows it', async () => {
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
      await view.rerender({ scrollTop: 295 * 30 })
      await view.rerender({ projection: loaded(200), cursorIndex: 300 })

      expect(events.oncursor).not.toHaveBeenCalled()
    })

    it('withdraws the window it asked for when the user scrolls back to the cursor', async () => {
      vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame'] })
      const events = { ...callbacks(), onshow: vi.fn() }
      // Rows 40 to 59 show, with the cursor on row 50.
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 50,
        compact: false,
        scrollTop: 40 * 30,
        viewport: 630,
        ...events,
      })
      await view.rerender({ scrollTop: 300 * 30 })
      vi.advanceTimersToNextFrame()
      expect(events.onshow).toHaveBeenLastCalledWith(300)
      await view.rerender({ scrollTop: 40 * 30 })
      vi.advanceTimersToNextFrame()

      // A row of the loaded window: the controller drops the request still on its way.
      expect(events.onshow).toHaveBeenLastCalledWith(50)
      expect(events.oncursor).not.toHaveBeenCalled()
    })

    it('still follows a scroll when a key the grid ignores is pressed meanwhile', async () => {
      vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame'] })
      const events = { ...callbacks(), onshow: vi.fn() }
      const scroller = scrollerAt(0)
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 0,
        compact: false,
        scroller,
        scrollTop: 0,
        viewport: 630,
        ...events,
      })
      await view.rerender({ scrollTop: 300 * 30 })
      vi.advanceTimersToNextFrame()
      await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), {
        key: 'Shift',
      })
      await view.rerender({ scrollTop: 310 * 30 })
      await view.rerender({ projection: loaded(200), cursorIndex: 300 })

      expect(scroller.scrollTop).toBe(0)
      expect(events.oncursor).toHaveBeenCalledWith(310)
    })

    it('stays in the window k moved the cursor into at its top edge', async () => {
      vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame'] })
      const events = { ...callbacks(), onshow: vi.fn() }
      const scroller = scrollerAt(200 * 30)
      // Rows 200 to 219 show, with the cursor on the first of them.
      const view = render(FinanceGrid, {
        projection: loaded(200),
        cursorIndex: 200,
        compact: false,
        scroller,
        scrollTop: 200 * 30,
        viewport: 630,
        ...events,
      })
      const grid = screen.getByRole('grid', { name: 'Financial results' })
      await fireEvent.keyDown(grid, { key: 'k' })
      expect(events.onmove).toHaveBeenCalledWith(-1)
      await view.rerender({ projection: loaded(0), cursorIndex: 199 })
      vi.advanceTimersToNextFrame()

      expect(scroller.scrollTop).toBe(199 * 30)
      expect(rowAt(199)).not.toBeNull()
      expect(grid.getAttribute('aria-activedescendant')).toBe('moneyflow-row-row-199')
      expect(events.onshow).not.toHaveBeenCalled()
      expect(events.oncursor).not.toHaveBeenCalled()
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

    it('reveals the cursor row again when the viewport shrinks under it', async () => {
      const scroller = scrollerAt(0)
      // Rows 0 to 19 show, with the cursor on the last of them.
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 19,
        compact: false,
        scroller,
        scrollTop: 0,
        viewport: 630,
        ...callbacks(),
      })
      await view.rerender({ viewport: 600 })
      expect(scroller.scrollTop).toBe(30)
    })

    it('keeps the cursor row on screen when rows change height at 640 px', async () => {
      const scroller = scrollerAt(0)
      // Rows 0 to 19 show at 30 px, with the cursor on the last of them.
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 19,
        compact: false,
        scroller,
        scrollTop: 0,
        viewport: 630,
        ...callbacks(),
      })
      await view.rerender({ compact: true })
      // At 52 px the cursor row's bottom is 20 rows plus the header down: 21 × 52 − 630.
      expect(scroller.scrollTop).toBe(21 * 52 - 630)
    })

    it('leaves a scrolled-away cursor alone when the viewport shrinks', async () => {
      const scroller = scrollerAt(0)
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 19,
        compact: false,
        scroller,
        scrollTop: 0,
        viewport: 630,
        ...callbacks(),
      })
      scroller.scrollTop = 100 * 30
      await view.rerender({ scrollTop: 100 * 30 })
      await view.rerender({ viewport: 600 })
      expect(scroller.scrollTop).toBe(100 * 30)
    })

    it('reveals the cursor row after a view change that keeps its index', async () => {
      const scroller = scrollerAt(0)
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 0,
        compact: false,
        scroller,
        scrollTop: 0,
        viewport: 630,
        ...callbacks(),
      })
      scroller.scrollTop = 100 * 30
      await view.rerender({ scrollTop: 100 * 30 })
      await view.rerender({ projection: { ...loaded(0), canonical_query: 'v=1&sort=amount' } })

      expect(scroller.scrollTop).toBe(0)
      const grid = screen.getByRole('grid', { name: 'Financial results' })
      const active = grid.getAttribute('aria-activedescendant')!
      expect(document.getElementById(active)).not.toBeNull()
    })

    it('reveals the cursor row on a key that cannot move it, such as Home on the first row', async () => {
      const scroller = scrollerAt(0)
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 0,
        compact: false,
        scroller,
        scrollTop: 0,
        viewport: 630,
        ...callbacks(),
      })
      scroller.scrollTop = 100 * 30
      await view.rerender({ scrollTop: 100 * 30 })
      await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), {
        key: 'Home',
      })
      expect(scroller.scrollTop).toBe(0)
    })

    it('names an active descendant only while the cursor row is rendered', async () => {
      const view = render(FinanceGrid, {
        projection: loaded(0),
        cursorIndex: 0,
        compact: false,
        scrollTop: 0,
        viewport: 630,
        ...callbacks(),
      })
      const grid = screen.getByRole('grid', { name: 'Financial results' })
      await view.rerender({ scrollTop: 100 * 30 })
      expect(grid.hasAttribute('aria-activedescendant')).toBe(false)

      await view.rerender({ scrollTop: 0 })
      expect(grid.getAttribute('aria-activedescendant')).toBe('moneyflow-row-row-0')
      expect(document.getElementById('moneyflow-row-row-0')).not.toBeNull()
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
