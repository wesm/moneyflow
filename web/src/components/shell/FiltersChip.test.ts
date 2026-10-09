import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import FiltersChip from './FiltersChip.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { testProjection } from '../../test/projection'

function projection(filters: ViewProjection['filters'], query = 'v=1'): ViewProjection {
  return testProjection({ filters, canonical_query: query })
}

describe('FiltersChip', () => {
  afterEach(cleanup)

  it('counts only the filters changed from their defaults', () => {
    const cases: [ViewProjection['filters'], string, string][] = [
      [{ show_hidden: true, show_transfers: false }, 'v=1', 'Filters'],
      [{ show_hidden: false, show_transfers: false }, 'hidden=0&v=1', 'Filters 1'],
      [{ show_hidden: true, show_transfers: true }, 'transfers=1&v=1', 'Filters 1'],
      [{ show_hidden: false, show_transfers: true }, 'hidden=0&transfers=1&v=1', 'Filters 2'],
    ]
    for (const [filters, query, label] of cases) {
      render(FiltersChip, { projection: projection(filters, query), onapply: vi.fn() })
      const trigger = screen.getByRole('button', { name: 'Filters' })
      expect(trigger.textContent?.replace(/\s+/g, ' ').trim()).toBe(label)
      expect(trigger.classList.contains('kit-filter-dropdown__btn--active')).toBe(
        label !== 'Filters',
      )
      cleanup()
    }
  })

  it('applies a second toggle on top of one still being applied', async () => {
    const onapply = vi.fn(() => new Promise<boolean>(() => undefined))
    render(FiltersChip, {
      projection: projection({ show_hidden: true, show_transfers: false }),
      onapply,
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Filters' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Show hidden transactions' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Show transfers' }))
    expect(onapply).toHaveBeenLastCalledWith({
      action: 'filters.apply',
      filters: { show_hidden: false, show_transfers: true },
    })
    expect(screen.getByRole('button', { name: 'Filters' }).textContent).toContain('2')
  })

  it('toggles a filter and keeps the current period', async () => {
    const onapply = vi.fn()
    render(FiltersChip, {
      projection: projection({
        show_hidden: true,
        show_transfers: false,
        date_range: { from: '2026-09-01', to: '2026-09-30' },
      }),
      onapply,
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Filters' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Show transfers' }))
    expect(onapply).toHaveBeenLastCalledWith({
      action: 'filters.apply',
      filters: {
        date_range: { start: '2026-09-01', end: '2026-09-30' },
        show_hidden: true,
        show_transfers: true,
      },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Show hidden transactions' }))
    expect(onapply).toHaveBeenLastCalledWith({
      action: 'filters.apply',
      filters: {
        date_range: { start: '2026-09-01', end: '2026-09-30' },
        show_hidden: false,
        show_transfers: false,
      },
    })
  })

  it('sends no date range for all time', async () => {
    const onapply = vi.fn()
    render(FiltersChip, {
      projection: projection({ show_hidden: false, show_transfers: false }),
      onapply,
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Filters' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Show hidden transactions' }))
    expect(onapply).toHaveBeenCalledWith({
      action: 'filters.apply',
      filters: { show_hidden: true, show_transfers: false },
    })
  })

  it('opens from the f key by focusing and pressing its trigger', async () => {
    const { component } = render(FiltersChip, {
      projection: projection({ show_hidden: false, show_transfers: false }),
      onapply: vi.fn(),
    })
    component.open()
    const trigger = screen.getByRole('button', { name: 'Filters' })
    expect(document.activeElement).toBe(trigger)
    expect(await screen.findByRole('button', { name: 'Show transfers' })).not.toBeNull()
  })
})
