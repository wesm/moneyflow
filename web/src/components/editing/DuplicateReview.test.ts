import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { DuplicateController } from '../../lib/controller/duplicates'
import DuplicateReview from './DuplicateReview.svelte'

describe('DuplicateReview', () => {
  afterEach(cleanup)

  it('renders accessible rows and routes keyboard review actions', async () => {
    const controller = duplicateController()
    const close = vi.fn()
    render(DuplicateReview, { controller, onclose: close })

    expect(screen.getByRole('dialog', { name: 'Duplicate transactions' })).not.toBeNull()
    expect(screen.getByRole('grid', { name: 'Likely duplicate transactions' })).not.toBeNull()
    expect(screen.getAllByText('Example Category')).toHaveLength(2)

    await fireEvent.keyDown(window, { key: 'j' })
    expect(controller.move).toHaveBeenCalledWith(1)
    await fireEvent.keyDown(window, { key: ' ' })
    expect(controller.toggleFocused).toHaveBeenCalledTimes(1)
    await fireEvent.keyDown(window, { key: 'h' })
    expect(controller.hideFocused).toHaveBeenCalledTimes(1)
    const select = screen.getByRole('button', { name: 'Select' })
    select.focus()
    await fireEvent.keyDown(select, { key: 'x' })
    expect(controller.requestDelete).toHaveBeenCalledTimes(1)
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(close).toHaveBeenCalledTimes(1)
  })

  it('shows focused transaction information with i and returns to review', async () => {
    const controller = duplicateController()
    render(DuplicateReview, { controller, onclose: vi.fn() })

    await fireEvent.keyDown(window, { key: 'i' })
    expect(screen.getByRole('region', { name: 'Transaction information' }).textContent).toContain(
      'Example Merchant',
    )
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('region', { name: 'Transaction information' })).toBeNull()
    expect(screen.getByRole('grid', { name: 'Likely duplicate transactions' })).not.toBeNull()
  })
})

function duplicateController(): DuplicateController {
  const row = {
    group_number: 1,
    target: { kind: 'transaction', identity: 'txn-1' },
    date: '2024-01-02',
    account: 'Account Name',
    merchant: 'Example Merchant',
    category: 'Example Category',
    group: 'Example Group',
    amount: {
      minor: '-1234',
      currency: 'USD',
      scale: 2,
      decimal: '-12.34',
      display: '-$12.34',
    },
    matching_label: 'Example Merchant',
    flags: { selected: false, hidden: false, pending: false },
  }
  return {
    state: {
      phase: 'ready',
      cursor: 0,
      announcement: '',
      confirmationCount: 0,
      projection: {
        version: '1',
        revision: '3',
        canonical_query: 'v=1',
        selection: 'mfsel1.empty',
        selection_count: 0,
        total_groups: 1,
        total_transactions: 2,
        group_window: { offset: 0, limit: 200, count: 1 },
        row_window: { offset: 0, limit: 200, count: 2 },
        groups: [
          { number: 1, rows: [row, { ...row, target: { ...row.target, identity: 'txn-2' } }] },
        ],
      },
    },
    open: vi.fn(async () => true),
    close: vi.fn(),
    move: vi.fn(),
    focus: vi.fn(),
    home: vi.fn(),
    end: vi.fn(),
    page: vi.fn(async () => true),
    focused: vi.fn(() => row),
    toggleFocused: vi.fn(async () => true),
    hideFocused: vi.fn(async () => true),
    requestDelete: vi.fn(),
    cancelDelete: vi.fn(),
    confirmDelete: vi.fn(async () => true),
  } as DuplicateController
}
