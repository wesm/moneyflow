import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import Sidebar from './Sidebar.svelte'
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
    profile_kind: 'monarch',
    view: {
      mode: 'aggregate',
      grouping: 'category',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
    },
    capabilities: capabilityIDs.map((id) => ({
      id,
      description: id,
      category: 'Views',
      available: id !== 'transactions.export',
    })),
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

describe('Sidebar', () => {
  afterEach(cleanup)

  it('names the profile provider under the brand', () => {
    render(Sidebar, props())
    expect(screen.getByText('Moneyflow')).not.toBeNull()
    expect(screen.getByText('Monarch')).not.toBeNull()
  })

  it('lists Explore and Maintain entries with their keys', () => {
    render(Sidebar, props())
    const nav = screen.getByRole('navigation', { name: 'Workspace' })
    const entries = (list: HTMLElement, expected: Array<[string, string, string]>) => {
      const buttons = within(list).getAllByRole('button')
      expect(buttons).toHaveLength(expected.length)
      for (const [index, [name, key, shortcut]] of expected.entries()) {
        expect(buttons[index]).toBe(within(list).getByRole('button', { name }))
        expect(buttons[index]?.querySelector('kbd')?.getAttribute('aria-label')).toBe(key)
        expect(buttons[index]?.getAttribute('aria-keyshortcuts')).toBe(shortcut)
      }
    }
    entries(within(nav).getByRole('list', { name: 'Explore' }), [
      ['Merchants', '1', '1'],
      ['Categories', '2', '2'],
      ['Groups', '3', '3'],
      ['Accounts', '4', '4'],
      ['Time', '5', '5'],
      ['All transactions', 'd', 'd'],
    ])
    entries(within(nav).getByRole('list', { name: 'Maintain' }), [
      ['Duplicates', 'D', 'Shift+D'],
      ['Export', 'E', 'Shift+E'],
      ['Categories and groups', 'C/G', 'Shift+C'],
    ])
  })

  it('marks only the active grouping as the current page', () => {
    render(Sidebar, props())
    const nav = screen.getByRole('navigation', { name: 'Workspace' })
    const current = within(nav)
      .getAllByRole('button')
      .filter((button) => button.getAttribute('aria-current') === 'page')
    expect(current).toEqual([within(nav).getByRole('button', { name: 'Categories' })])
  })

  it('runs the entry action on click and disables unavailable entries', async () => {
    const onrun = vi.fn()
    render(Sidebar, props({ onrun }))
    await fireEvent.click(screen.getByRole('button', { name: 'Groups' }))
    expect(onrun).toHaveBeenCalledWith({
      kind: 'apply',
      action: 'view.set-grouping',
      dimension: 'group',
    })
    const exportButton = screen.getByRole('button', { name: 'Export' })
    expect(exportButton.hasAttribute('disabled')).toBe(true)
  })

  it('pins the pending card, provider refresh, and theme toggle to the bottom', async () => {
    const onreview = vi.fn()
    render(Sidebar, props({ onreview }))
    const status = screen.getByRole('status', { name: 'Pending profile changes' })
    expect(status.textContent).toContain('3 pending edits · 27 transactions')
    await fireEvent.click(screen.getByRole('button', { name: 'Review pending changes' }))
    expect(onreview).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: 'Refresh provider data' })).not.toBeNull()
    expect(screen.getByRole('button', { name: /Change theme/ })).not.toBeNull()
  })

  it('offers Amazon import instead of provider refresh on Amazon profiles', async () => {
    const onrun = vi.fn()
    render(Sidebar, props({ onrun, projection: projection({ profile_kind: 'amazon' }) }))
    expect(screen.getByText('Amazon')).not.toBeNull()
    expect(screen.queryByRole('button', { name: 'Refresh provider data' })).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Import Amazon orders' }))
    expect(onrun).toHaveBeenCalledWith({ kind: 'local', action: 'provider.refresh' })
  })
})
