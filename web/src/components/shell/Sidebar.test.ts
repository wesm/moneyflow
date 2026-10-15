import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import Sidebar from './Sidebar.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { status } from '../../lib/shell/status.svelte'
import { navCapabilityIDs, testCapabilities } from '../../test/capabilities'
import { stubMedia } from '../../test/media'
import { testProjection } from '../../test/projection'
import { stubProvider } from '../../test/provider'

function projection(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return testProjection({
    profile_kind: 'monarch',
    view: {
      mode: 'aggregate',
      grouping: 'category',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
      sort_fields: ['merchant', 'count', 'amount'],
      drill_depth: 0,
      root: { mode: 'aggregate', grouping: 'category', time_granularity: 'year' },
    },
    capabilities: testCapabilities(navCapabilityIDs, ['transactions.export']),
    ...overrides,
  })
}

function props(overrides: Record<string, unknown> = {}) {
  return {
    projection: projection(),
    pending: { active_operations: 3, inactive_operations: 0, affected_transactions: 27 },
    provider: stubProvider(true),
    onrun: vi.fn(),
    onreview: vi.fn(),
    onwrite: vi.fn(),
    ...overrides,
  }
}

function entries(group: string): Array<string | undefined> {
  const nav = screen.getByRole('navigation', { name: 'Workspace' })
  return within(within(nav).getByRole('list', { name: group }))
    .getAllByRole('link')
    .map((link) => link.textContent?.trim())
}

describe('Sidebar', () => {
  afterEach(() => {
    cleanup()
    status.clear()
  })

  it('names the profile provider under the brand', () => {
    render(Sidebar, props())
    expect(screen.getByText('Moneyflow')).not.toBeNull()
    expect(screen.getByText('Monarch')).not.toBeNull()
  })

  it('lists the Explore and Maintain entries as links', () => {
    render(Sidebar, props())
    expect(entries('Explore')).toEqual([
      'Merchants',
      'Categories',
      'Groups',
      'Accounts',
      'Time',
      'All transactions',
    ])
    expect(entries('Maintain')).toEqual(['Duplicates', 'Export', 'Edit categories', 'Edit groups'])
  })

  it('marks only the active grouping as the current page', () => {
    render(Sidebar, props())
    const nav = screen.getByRole('navigation', { name: 'Workspace' })
    const current = within(nav)
      .getAllByRole('link')
      .filter((link) => link.getAttribute('aria-current') === 'page')
    expect(current).toEqual([within(nav).getByRole('link', { name: 'Categories' })])
  })

  it('runs the entry action on click instead of following the link', async () => {
    const onrun = vi.fn()
    render(Sidebar, props({ onrun }))
    const followed = await fireEvent.click(screen.getByRole('link', { name: 'Groups' }))
    expect(followed).toBe(false)
    expect(onrun).toHaveBeenCalledWith({
      kind: 'apply',
      action: 'view.set-grouping',
      dimension: 'group',
    })
  })

  it('focuses a clicked entry before it runs, as WebKit does not', async () => {
    let focused: Element | null = null
    const onrun = vi.fn(() => {
      focused = document.activeElement
    })
    render(Sidebar, props({ onrun }))
    const item = screen.getByRole('link', { name: 'Edit categories' })
    await fireEvent.click(item)
    expect(onrun).toHaveBeenCalledTimes(1)
    expect(focused).toBe(item)
  })

  it('explains an unavailable entry in the status line instead of running it', async () => {
    const onrun = vi.fn()
    render(Sidebar, props({ onrun }))
    const followed = await fireEvent.click(screen.getByRole('link', { name: 'Export' }))
    expect(followed).toBe(false)
    expect(onrun).not.toHaveBeenCalled()
    expect(status.notice).toBe('Not available in this view')
  })

  it('pins the pending card, provider refresh, and theme toggle to the bottom', async () => {
    const onreview = vi.fn()
    render(Sidebar, props({ onreview }))
    const pending = screen.getByRole('status', { name: 'Pending profile changes' })
    expect(pending.textContent).toContain('3 pending edits · 27 transactions')
    await fireEvent.click(screen.getByRole('button', { name: 'Review pending changes' }))
    expect(onreview).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: 'Refresh provider data' })).not.toBeNull()
    expect(screen.getByRole('button', { name: /Change theme/ })).not.toBeNull()
  })

  it('hides the review button and provider refresh when there is nothing to act on', () => {
    for (const profile_kind of ['local', 'csv'] as const) {
      render(
        Sidebar,
        props({
          projection: projection({ profile_kind }),
          pending: { active_operations: 0, inactive_operations: 0, affected_transactions: 0 },
        }),
      )
      expect(screen.getByRole('status', { name: 'Pending profile changes' })).not.toBeNull()
      expect(screen.queryByRole('button', { name: 'Review pending changes' })).toBeNull()
      expect(screen.queryByRole('button', { name: 'Refresh provider data' })).toBeNull()
      expect(screen.getByRole('button', { name: /Change theme/ })).not.toBeNull()
      cleanup()
    }
  })

  it('offers Amazon import instead of provider refresh on Amazon profiles', async () => {
    const onrun = vi.fn()
    render(Sidebar, props({ onrun, projection: projection({ profile_kind: 'amazon' }) }))
    expect(screen.getByText('Amazon')).not.toBeNull()
    expect(screen.queryByRole('button', { name: 'Refresh provider data' })).toBeNull()
    await fireEvent.click(screen.getByRole('link', { name: 'Import Amazon orders' }))
    expect(onrun).toHaveBeenCalledWith({ kind: 'local', action: 'provider.refresh' })
  })

  it('reports the full footer while it shows', () => {
    const onfooter = vi.fn()
    const rendered = render(Sidebar, props({ onfooter }))
    const footer = onfooter.mock.lastCall?.[0] as HTMLElement | undefined
    expect(footer?.textContent).toContain('3 pending edits')
    rendered.unmount()
    expect(onfooter).toHaveBeenLastCalledWith(undefined)
  })
})

describe('Sidebar at rail widths', () => {
  let restore: () => void

  beforeEach(() => {
    restore = stubMedia((query) => query.includes('< 1440px'))
  })

  afterEach(() => {
    cleanup()
    restore()
  })

  it('shows the entries as icons and leaves the pending text to the status line', () => {
    const onfooter = vi.fn()
    render(Sidebar, props({ onfooter }))
    const nav = screen.getByRole('navigation', { name: 'Workspace' })
    expect(within(nav).getByRole('link', { name: 'Merchants' })).not.toBeNull()
    expect(screen.queryByText('Moneyflow')).toBeNull()
    expect(screen.queryByRole('status', { name: 'Pending profile changes' })).toBeNull()
    expect(onfooter).not.toHaveBeenCalled()
  })

  it('opens review from a pending icon that counts pending edits', async () => {
    const onreview = vi.fn()
    render(Sidebar, props({ onreview }))
    const review = screen.getByRole('button', { name: 'Review pending changes' })
    expect(review.closest('li')?.textContent).toContain('3')
    await fireEvent.click(review)
    expect(onreview).toHaveBeenCalledTimes(1)
    await fireEvent.focusIn(review)
    expect((await screen.findByRole('tooltip')).textContent?.trim()).toBe(
      'Review pending changes · w',
    )
  })

  it('offers no review without pending edits, like the pending card', async () => {
    const onreview = vi.fn()
    render(
      Sidebar,
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

  it('offers no review and no count when pending edits cannot be reviewed here', async () => {
    const onreview = vi.fn()
    const capabilities = testCapabilities(navCapabilityIDs, ['changes.review'])
    render(Sidebar, props({ onreview, projection: projection({ capabilities }) }))
    const review = screen.getByRole('button', { name: 'Review pending changes' })
    expect(review.getAttribute('aria-disabled')).toBe('true')
    await fireEvent.click(review)
    expect(onreview).not.toHaveBeenCalled()
    expect(review.closest('li')?.querySelector('.mf-rail__count')).toBeNull()
    await fireEvent.focusIn(review)
    expect((await screen.findByRole('tooltip')).textContent?.trim()).toBe(
      'Review pending changes · Not available in this view',
    )
  })

  it('shows write progress on the pending icon while a write runs', async () => {
    render(Sidebar, props({ write: { completed: 2, total: 5 } }))
    const review = screen.getByRole('button', { name: 'Review pending changes' })
    await fireEvent.focusIn(review)
    expect((await screen.findByRole('tooltip')).textContent?.trim()).toBe('Write 2/5 · w')
  })

  it('keeps provider refresh and the theme toggle, and omits refresh on local profiles', () => {
    render(Sidebar, props())
    expect(screen.getByRole('button', { name: 'Refresh provider data' })).not.toBeNull()
    expect(screen.getByRole('button', { name: /Change theme/ })).not.toBeNull()
    cleanup()
    render(Sidebar, props({ projection: projection({ profile_kind: 'local' }) }))
    expect(screen.queryByRole('button', { name: 'Refresh provider data' })).toBeNull()
  })
})
