import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PendingCard from './PendingCard.svelte'

const pending = { active_operations: 3, inactive_operations: 1, affected_transactions: 27 }

describe('PendingCard', () => {
  afterEach(cleanup)

  it('announces pending edits, affected transactions, and redo history', () => {
    render(PendingCard, { pending, reviewAvailable: true, onreview: vi.fn(), onwrite: vi.fn() })
    const status = screen.getByRole('status', { name: 'Pending profile changes' })
    expect(status.textContent?.trim()).toBe('3 pending edits · 27 transactions · 1 redo')
  })

  it('opens review from its button and disables it when review is unavailable', async () => {
    const onreview = vi.fn()
    const rendered = render(PendingCard, {
      pending,
      reviewAvailable: true,
      onreview,
      onwrite: vi.fn(),
    })
    const review = screen.getByRole('button', { name: 'Review pending changes' })
    expect(review.textContent).toContain('w')
    await fireEvent.click(review)
    expect(onreview).toHaveBeenCalledTimes(1)

    await rendered.rerender({ pending, reviewAvailable: false, onreview, onwrite: vi.fn() })
    expect(
      screen.getByRole('button', { name: 'Review pending changes' }).hasAttribute('disabled'),
    ).toBe(true)
  })

  it('focuses Review and Write before they run, as WebKit does not', async () => {
    let focused: Element | null = null
    const record = () => {
      focused = document.activeElement
    }
    render(PendingCard, {
      pending,
      reviewAvailable: true,
      write: { completed: 2, total: 5 },
      onreview: vi.fn(record),
      onwrite: vi.fn(record),
    })
    const review = screen.getByRole('button', { name: 'Review pending changes' })
    await fireEvent.click(review)
    expect(focused).toBe(review)
    const write = screen.getByRole('button', { name: 'Write 2/5' })
    await fireEvent.click(write)
    expect(focused).toBe(write)
  })

  it('shows write progress while a provider write runs', async () => {
    const onwrite = vi.fn()
    render(PendingCard, {
      pending,
      reviewAvailable: false,
      write: { completed: 2, total: 5 },
      onreview: vi.fn(),
      onwrite,
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Write 2/5' }))
    expect(onwrite).toHaveBeenCalledTimes(1)
  })

  it('offers no review when nothing is pending', () => {
    const empty = { active_operations: 0, inactive_operations: 0, affected_transactions: 0 }
    render(PendingCard, {
      pending: empty,
      reviewAvailable: true,
      onreview: vi.fn(),
      onwrite: vi.fn(),
    })
    expect(
      screen.getByRole('status', { name: 'Pending profile changes' }).textContent?.trim(),
    ).toBe('0 pending edits · 0 transactions')
    expect(screen.queryByRole('button', { name: 'Review pending changes' })).toBeNull()
  })

  it('offers the last finished write while nothing is pending', async () => {
    const empty = { active_operations: 0, inactive_operations: 0, affected_transactions: 0 }
    const onwrite = vi.fn()
    const rendered = render(PendingCard, {
      pending: empty,
      reviewAvailable: false,
      lastWrite: true,
      onreview: vi.fn(),
      onwrite,
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Last write' }))
    expect(onwrite).toHaveBeenCalledTimes(1)

    // New pending edits take the card's action; the last write gives way to Review.
    await rendered.rerender({
      pending,
      reviewAvailable: true,
      lastWrite: true,
      onreview: vi.fn(),
      onwrite,
    })
    expect(screen.queryByRole('button', { name: 'Last write' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Review pending changes' })).not.toBeNull()
  })

  it('hides the write button when no write runs', () => {
    render(PendingCard, { pending, reviewAvailable: true, onreview: vi.fn(), onwrite: vi.fn() })
    expect(screen.queryByRole('button', { name: /^Write/ })).toBeNull()
  })
})
