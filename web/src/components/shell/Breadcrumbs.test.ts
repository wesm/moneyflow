import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import Breadcrumbs from './Breadcrumbs.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { testProjection } from '../../test/projection'

const merchantDrill = 'drill=merchant%3AUSD%3A2%3Aexample-key'
const categoryDrill = 'drill=category%3AUSD%3A2%3Aexample-category'

function drilled(overrides: Partial<ViewProjection['view']> = {}): ViewProjection {
  const base = testProjection()
  return testProjection({
    canonical_query: `v=1&mode=detail&${merchantDrill}&${categoryDrill}`,
    view: { ...base.view, mode: 'detail', grouping: 'merchant', ...overrides },
    breadcrumbs: [
      { dimension: 'merchant', label: 'Corner Grocery' },
      { dimension: 'category', label: 'Groceries' },
    ],
  })
}

describe('Breadcrumbs', () => {
  afterEach(cleanup)

  it('lists the trail in a Breadcrumb landmark with the last crumb as the current page', () => {
    render(Breadcrumbs, { projection: drilled(), onback: vi.fn() })
    const nav = screen.getByRole('navigation', { name: 'Breadcrumb' })
    const items = within(nav).getAllByRole('listitem')
    expect(items.map((item) => item.textContent?.replace(/\s+/g, ' ').trim())).toEqual([
      'Merchants',
      '› Corner Grocery',
      '› Groceries',
    ])
    const current = within(nav).getByText('Groceries')
    expect(current.getAttribute('aria-current')).toBe('page')
    expect(within(nav).queryByRole('button', { name: 'Groceries' })).toBeNull()
    for (const separator of nav.querySelectorAll('.mf-breadcrumbs__separator')) {
      expect(separator.getAttribute('aria-hidden')).toBe('true')
    }
  })

  it('returns to the level of the crumb pressed', async () => {
    const onback = vi.fn()
    render(Breadcrumbs, { projection: drilled(), onback })
    await fireEvent.click(screen.getByRole('button', { name: 'Merchants' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Corner Grocery' }))
    expect(onback.mock.calls).toEqual([[0], [1]])
  })

  it('makes the last drill frame a button once the view is broken down', async () => {
    const onback = vi.fn()
    render(Breadcrumbs, { projection: drilled({ sub_grouping: 'account' }), onback })
    expect(screen.getByText('by Account').getAttribute('aria-current')).toBe('page')
    await fireEvent.click(screen.getByRole('button', { name: 'Groceries' }))
    expect(onback).toHaveBeenCalledWith(2)
  })

  it('shows the top-level grouping as text with nothing to press', () => {
    render(Breadcrumbs, {
      projection: testProjection({ view: { ...testProjection().view, mode: 'aggregate' } }),
      onback: vi.fn(),
    })
    expect(screen.getByText('Merchants').getAttribute('aria-current')).toBe('page')
    expect(screen.queryByRole('button')).toBeNull()
  })
})
