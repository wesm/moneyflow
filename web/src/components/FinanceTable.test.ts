import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import FinanceTable from './FinanceTable.svelte'
import { testProjection } from '../test/projection'

const callbacks = () => ({
  onmove: vi.fn(),
  onhome: vi.fn(),
  onactivate: vi.fn(),
  onselect: vi.fn(),
  oninformation: vi.fn(),
})
describe('FinanceTable', () => {
  afterEach(cleanup)
  it('renders exact detail money in the permanent accessible grid', async () => {
    const events = callbacks()
    render(FinanceTable, {
      projection: testProjection({
        view: {
          mode: 'detail',
          grouping: 'merchant',
          time_granularity: 'year',
          sort_field: 'date',
          sort_direction: 'desc',
        },
      }),
      cursorIndex: 0,
      ...events,
    })
    const grid = screen.getByRole('grid', { name: 'Financial results' })
    expect(grid.getAttribute('aria-rowcount')).toBe('2')
    expect(
      screen.getByRole('row', { name: /Example Merchant/ }).getAttribute('aria-selected'),
    ).toBe('true')
    expect(screen.getByText('-$12.34')).not.toBeNull()
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
    await fireEvent.doubleClick(screen.getByRole('row', { name: /Example Merchant/ }))
    expect(events.onactivate).not.toHaveBeenCalled()
    expect(events.oninformation).toHaveBeenCalledWith('row-0')
    await fireEvent.click(screen.getByRole('row', { name: /Example Merchant/ }))
    expect(events.onselect).toHaveBeenCalledWith('row-0', 'transaction')
  })
  it('renders a bounded Amazon match indicator with an explicit details action', async () => {
    const events = callbacks()
    const projection = testProjection()
    render(FinanceTable, {
      projection: testProjection({
        amazon_match_column: true,
        detail_rows: projection.detail_rows!.map((row) => ({
          ...row,
          amazon_match: {
            class: 'exact',
            confidence: 'high',
            first_product: 'Example Product',
            total_matches: 1,
          },
        })),
      }),
      cursorIndex: 0,
      ...events,
    })
    expect(screen.getByRole('columnheader', { name: 'Amazon match' })).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: /Example Product/ }))
    expect(events.oninformation).toHaveBeenCalledWith('row-0')
    expect(events.onselect).not.toHaveBeenCalled()
  })
  it('renders aggregate columns and empty results', async () => {
    const events = callbacks()
    render(FinanceTable, {
      projection: testProjection({
        detail_rows: null,
        aggregate_rows: [
          {
            index: 0,
            identity: 'agg-0',
            dimension: 'merchant',
            label: 'Example Merchant',
            count: 2,
            total: {
              minor: '-200',
              currency: 'USD',
              scale: 2,
              decimal: '-2.00',
              display: '-$2.00',
            },
            share_tenths: 1000,
            flags: { selected: false, hidden: false, pending: false },
          },
        ],
      }),
      cursorIndex: 0,
      ...events,
    })
    expect(screen.getByRole('columnheader', { name: 'merchant' })).not.toBeNull()
    await fireEvent.doubleClick(screen.getByRole('row', { name: /Example Merchant/ }))
    expect(events.onactivate).toHaveBeenCalledWith('agg-0', 'aggregate')
    cleanup()
    render(FinanceTable, {
      projection: testProjection({
        total_rows: 0,
        detail_rows: [],
        window: { offset: 0, limit: 200, count: 0 },
      }),
      cursorIndex: 0,
      ...events,
    })
    expect(screen.getByText('No transactions')).not.toBeNull()
  })
  it('marks the time header with the server sort direction', () => {
    render(FinanceTable, {
      projection: testProjection({
        view: {
          mode: 'aggregate',
          grouping: 'time',
          time_granularity: 'month',
          sort_field: 'time_period',
          sort_direction: 'asc',
        },
        detail_rows: null,
        aggregate_rows: [
          {
            index: 0,
            identity: 'time-0',
            dimension: 'time',
            label: 'January 2026',
            count: 1,
            total: {
              minor: '-100',
              currency: 'USD',
              scale: 2,
              decimal: '-1.00',
              display: '-$1.00',
            },
            share_tenths: 1000,
            flags: { selected: false, hidden: false, pending: false },
          },
        ],
      }),
      cursorIndex: 0,
      ...callbacks(),
    })
    expect(screen.getByRole('columnheader', { name: 'time' }).getAttribute('aria-sort')).toBe(
      'ascending',
    )
  })

  it('labels pending rows with visible text instead of color alone', () => {
    render(FinanceTable, {
      projection: testProjection({
        detail_rows: testProjection().detail_rows!.map((row) => ({
          ...row,
          flags: { ...row.flags, pending: true },
        })),
      }),
      cursorIndex: 0,
      ...callbacks(),
    })
    expect(screen.getByText('pending')).not.toBeNull()
  })
})
