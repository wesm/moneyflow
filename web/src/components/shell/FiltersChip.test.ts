import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import FiltersChip from './FiltersChip.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { testProjection } from '../../test/projection'

function projection(filters: ViewProjection['filters']): ViewProjection {
  return testProjection({ filters })
}

describe('FiltersChip', () => {
  afterEach(cleanup)

  it('counts the enabled filters on its badge', () => {
    render(FiltersChip, {
      projection: projection({ show_hidden: true, show_transfers: true }),
      onapply: vi.fn(),
    })
    expect(screen.getByRole('button', { name: 'Filters' }).textContent).toContain('2')
    cleanup()
    render(FiltersChip, {
      projection: projection({ show_hidden: false, show_transfers: false }),
      onapply: vi.fn(),
    })
    expect(screen.getByRole('button', { name: 'Filters' }).textContent?.trim()).toBe('Filters')
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
