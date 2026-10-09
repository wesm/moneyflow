import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import SidebarRail from './SidebarRail.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { navCapabilityIDs, testCapabilities } from '../../test/capabilities'
import { testProjection } from '../../test/projection'
import { stubProvider } from '../../test/provider'

function projection(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return testProjection({
    profile_kind: 'monarch',
    view: {
      mode: 'aggregate',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
    },
    capabilities: testCapabilities(navCapabilityIDs, ['view.find-duplicates']),
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

  it('runs entries and names their keys', async () => {
    const onrun = vi.fn()
    render(SidebarRail, props({ onrun }))
    const detail = screen.getByRole('button', { name: 'All transactions' })
    await fireEvent.click(detail)
    expect(onrun).toHaveBeenCalledWith({ kind: 'apply', action: 'view.show-detail' })
    expect(detail.getAttribute('aria-keyshortcuts')).toBe('d')
    expect(
      screen.getByRole('button', { name: 'Edit groups' }).getAttribute('aria-keyshortcuts'),
    ).toBe('Shift+G')
  })

  it('keeps unavailable entries focusable and explains them in the tooltip', async () => {
    const onrun = vi.fn()
    render(SidebarRail, props({ onrun }))
    const duplicates = screen.getByRole('button', { name: 'Duplicates' })
    expect(duplicates.getAttribute('aria-disabled')).toBe('true')
    expect(duplicates.hasAttribute('disabled')).toBe(false)
    await fireEvent.click(duplicates)
    expect(onrun).not.toHaveBeenCalled()
    await fireEvent.focusIn(duplicates)
    expect((await screen.findByRole('tooltip')).textContent?.trim()).toBe(
      'Duplicates · Not available in this view',
    )
  })

  it('opens review from a pending icon that counts pending edits', async () => {
    const onreview = vi.fn()
    render(SidebarRail, props({ onreview }))
    const review = screen.getByRole('button', { name: 'Review pending changes' })
    expect(review.closest('li')?.textContent).toContain('2')
    await fireEvent.click(review)
    expect(onreview).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/pending edit/)).toBeNull()
    await fireEvent.focusIn(review)
    expect((await screen.findByRole('tooltip')).textContent?.trim()).toBe(
      'Review pending changes · w',
    )
  })

  it('offers no review without pending edits, like the sidebar card', async () => {
    const onreview = vi.fn()
    render(
      SidebarRail,
      props({
        onreview,
        pending: { active_operations: 0, inactive_operations: 1, affected_transactions: 0 },
      }),
    )
    const review = screen.getByRole('button', { name: 'Review pending changes' })
    expect(review.getAttribute('aria-disabled')).toBe('true')
    await fireEvent.click(review)
    expect(onreview).not.toHaveBeenCalled()
    expect(review.closest('li')?.querySelector('.mf-rail__count')).toBeNull()
    await fireEvent.focusIn(review)
    expect((await screen.findByRole('tooltip')).textContent?.trim()).toBe('No pending edits')
  })

  it('shows write progress on the pending icon while a write runs', async () => {
    render(SidebarRail, props({ write: { completed: 2, total: 5 } }))
    const review = screen.getByRole('button', { name: 'Review pending changes' })
    expect(review.closest('li')?.textContent).toContain('2')
    await fireEvent.focusIn(review)
    expect((await screen.findByRole('tooltip')).textContent?.trim()).toBe('Write 2/5 · w')
  })

  it('keeps provider refresh and the theme toggle', () => {
    render(SidebarRail, props())
    expect(screen.getByRole('button', { name: 'Refresh provider data' })).not.toBeNull()
    expect(screen.getByRole('button', { name: /Change theme/ })).not.toBeNull()
  })

  it('omits provider refresh on local profiles', () => {
    render(SidebarRail, props({ projection: projection({ profile_kind: 'local' }) }))
    expect(screen.queryByRole('button', { name: 'Refresh provider data' })).toBeNull()
  })
})
