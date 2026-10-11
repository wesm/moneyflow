import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import BreakDownBy from './BreakDownBy.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { testProjection } from '../../test/projection'

function drilled(subGrouping?: string): ViewProjection {
  const base = testProjection()
  return testProjection({
    view: {
      ...base.view,
      mode: 'detail',
      drill_depth: 1,
      ...(subGrouping === undefined ? {} : { sub_grouping: subGrouping }),
    },
    breadcrumbs: [{ dimension: 'merchant', label: 'Corner Grocery' }],
  })
}

function radios(): string[] {
  return screen.getAllByRole('radio').map((radio) => radio.textContent?.trim() ?? '')
}

describe('BreakDownBy', () => {
  afterEach(cleanup)

  it('offers Transactions and the dimensions not yet drilled, with Transactions selected', () => {
    render(BreakDownBy, { projection: drilled(), onbreakdown: vi.fn(), ontransactions: vi.fn() })
    expect(screen.getByRole('radiogroup', { name: 'Break down by' })).not.toBeNull()
    expect(radios()).toEqual(['Transactions', 'Category', 'Group', 'Account', 'Time'])
    expect(screen.getByRole('radio', { name: 'Transactions' }).getAttribute('aria-checked')).toBe(
      'true',
    )
  })

  it('breaks the drill down by the dimension chosen', async () => {
    const onbreakdown = vi.fn()
    render(BreakDownBy, { projection: drilled(), onbreakdown, ontransactions: vi.fn() })
    await fireEvent.click(screen.getByRole('radio', { name: 'Account' }))
    expect(onbreakdown).toHaveBeenCalledWith('account')
  })

  it('returns to the transactions from a breakdown', async () => {
    const ontransactions = vi.fn()
    render(BreakDownBy, { projection: drilled('time'), onbreakdown: vi.fn(), ontransactions })
    expect(screen.getByRole('radio', { name: 'Time' }).getAttribute('aria-checked')).toBe('true')
    await fireEvent.click(screen.getByRole('radio', { name: 'Transactions' }))
    expect(ontransactions).toHaveBeenCalledOnce()
  })

  it('is hidden at the top level', () => {
    render(BreakDownBy, {
      projection: testProjection({ view: { ...testProjection().view, mode: 'aggregate' } }),
      onbreakdown: vi.fn(),
      ontransactions: vi.fn(),
    })
    expect(screen.queryByRole('radiogroup')).toBeNull()
  })
})
