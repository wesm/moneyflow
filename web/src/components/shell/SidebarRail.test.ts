import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import SidebarRail from './SidebarRail.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { testProjection } from '../../test/projection'
import { stubProvider } from '../../test/provider'

const capabilityIDs = [
  'view.set-grouping',
  'view.show-detail',
  'view.find-duplicates',
  'transactions.export',
  'category.manage',
  'changes.review',
  'provider.refresh',
]

function projection(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return testProjection({
    view: {
      mode: 'aggregate',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
    },
    capabilities: capabilityIDs.map((id) => ({
      id,
      description: id,
      category: 'Views',
      available: id !== 'view.find-duplicates',
    })),
    ...overrides,
  })
}

function props(overrides: Record<string, unknown> = {}) {
  return {
    projection: projection(),
    pending: { active_operations: 2, inactive_operations: 0, affected_transactions: 5 },
    provider: stubProvider(true),
    onrun: vi.fn(),
    onreview: vi.fn(),
    ...overrides,
  }
}

describe('SidebarRail', () => {
  afterEach(cleanup)

  it('renders every entry as a named icon button with its key in a tooltip', async () => {
    render(SidebarRail, props())
    const nav = screen.getByRole('navigation', { name: 'Workspace' })
    const merchants = within(nav).getByRole('button', { name: 'Merchants' })
    expect(merchants.getAttribute('aria-current')).toBe('page')
    expect(
      within(nav).getByRole('button', { name: 'Categories' }).hasAttribute('aria-current'),
    ).toBe(false)
    await fireEvent.focusIn(merchants)
    expect((await screen.findByRole('tooltip')).textContent?.trim()).toBe('Merchants · 1')
  })

  it('runs entries and disables unavailable ones', async () => {
    const onrun = vi.fn()
    render(SidebarRail, props({ onrun }))
    await fireEvent.click(screen.getByRole('button', { name: 'All transactions' }))
    expect(onrun).toHaveBeenCalledWith({ kind: 'apply', action: 'view.show-detail' })
    expect(screen.getByRole('button', { name: 'Duplicates' }).hasAttribute('disabled')).toBe(true)
  })

  it('opens review from a pending icon that counts pending edits', async () => {
    const onreview = vi.fn()
    render(SidebarRail, props({ onreview }))
    const review = screen.getByRole('button', { name: 'Review pending changes' })
    expect(review.closest('li')?.textContent).toContain('2')
    await fireEvent.click(review)
    expect(onreview).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/pending edit/)).toBeNull()
  })

  it('keeps provider refresh and the theme toggle', () => {
    render(SidebarRail, props())
    expect(screen.getByRole('button', { name: 'Refresh provider data' })).not.toBeNull()
    expect(screen.getByRole('button', { name: /Change theme/ })).not.toBeNull()
  })
})
