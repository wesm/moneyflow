import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import FinanceGrid from './FinanceGrid.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { testAggregateRow, testMoney, testProjection } from '../../test/projection'

const callbacks = () => ({
  onmove: vi.fn(),
  onhome: vi.fn(),
  ontoggle: vi.fn(),
  onactivate: vi.fn(),
  oninformation: vi.fn(),
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

const headers = () =>
  screen.getAllByRole('columnheader').map((header) => header.textContent?.trim())
const cell = (row: HTMLElement, key: string) =>
  row.querySelector<HTMLElement>(`[role="gridcell"][data-column="${key}"]`)

describe('FinanceGrid', () => {
  afterEach(cleanup)

  it('renders transaction rows with today’s pointer behavior', async () => {
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
    expect(headers()).toEqual([
      'Selection',
      'Date',
      'Merchant',
      'Category',
      'Account',
      'Amount',
      'Flags',
    ])
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
    await fireEvent.doubleClick(row)
    expect(events.onactivate).not.toHaveBeenCalled()
    expect(events.oninformation).toHaveBeenCalledWith('row-0')
    await fireEvent.click(row)
    expect(events.ontoggle).toHaveBeenCalledWith('row-0', 'transaction')
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
      'Selection',
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
    await fireEvent.doubleClick(row)
    expect(events.onactivate).toHaveBeenCalledWith('agg-0', 'aggregate')

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
      'Selection',
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
