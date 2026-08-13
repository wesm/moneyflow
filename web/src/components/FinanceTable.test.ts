import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import FinanceTable from './FinanceTable.svelte'
import { testProjection } from '../test/projection'

const callbacks = () => ({
  onmove: vi.fn(),
  onhome: vi.fn(),
  onactivate: vi.fn(),
  onselect: vi.fn(),
})
describe('FinanceTable', () => {
  afterEach(cleanup)
  it('renders exact detail money in the permanent accessible grid', async () => {
    const events = callbacks()
    render(FinanceTable, { projection: testProjection(), cursorIndex: 0, ...events })
    const grid = screen.getByRole('grid', { name: 'Financial results' })
    expect(grid.getAttribute('aria-rowcount')).toBe('2')
    expect(
      screen.getByRole('row', { name: /Example Merchant/ }).getAttribute('aria-selected'),
    ).toBe('true')
    expect(screen.getByText('-$12.34')).not.toBeNull()
    expect(screen.getByRole('columnheader', { name: 'Amount' }).getAttribute('aria-sort')).toBe(
      'descending',
    )
    await fireEvent.keyDown(grid, { key: 'j' })
    expect(events.onmove).toHaveBeenCalledWith(1)
    await fireEvent.keyDown(grid, { key: 'Home' })
    expect(events.onhome).toHaveBeenCalled()
    await fireEvent.keyDown(grid, { key: 'End' })
    expect(events.onmove).toHaveBeenCalledTimes(1)
    await fireEvent.doubleClick(screen.getByRole('row', { name: /Example Merchant/ }))
    expect(events.onactivate).toHaveBeenCalledWith('row-0', 'detail')
  })
  it('renders aggregate columns and empty results', () => {
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
})
