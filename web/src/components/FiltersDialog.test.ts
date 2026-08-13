import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import FiltersDialog from './FiltersDialog.svelte'
import { testProjection } from '../test/projection'

describe('FiltersDialog', () => {
  afterEach(cleanup)

  it('stages values, cancels without applying, and focuses the first control', async () => {
    const onapply = vi.fn()
    const onclose = vi.fn()
    render(FiltersDialog, { projection: testProjection(), onapply, onclose })
    expect(document.activeElement).toBe(screen.getByRole('button', { name: /All time/ }))
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Show hidden transactions' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onapply).not.toHaveBeenCalled()
    expect(onclose).toHaveBeenCalledTimes(1)
  })

  it('applies one inclusive date and visibility transition', async () => {
    const onapply = vi.fn(async () => undefined)
    render(FiltersDialog, {
      projection: testProjection({
        filters: {
          date_range: { from: '2026-08-01', to: '2026-08-13' },
          show_hidden: false,
          show_transfers: true,
        },
      }),
      onapply,
      onclose: vi.fn(),
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Apply filters' }))
    expect(onapply).toHaveBeenCalledWith({
      action: 'filters.apply',
      filters: {
        date_range: { start: '2026-08-01', end: '2026-08-13' },
        show_hidden: false,
        show_transfers: true,
      },
    })
  })
})
