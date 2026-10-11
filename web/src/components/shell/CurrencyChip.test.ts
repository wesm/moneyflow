import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import CurrencyChip from './CurrencyChip.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { testProjection } from '../../test/projection'

const usd = { currency: 'USD', scale: 2, count: 1234, first: '2024-01-01', latest: '2026-10-07' }
const eur = { currency: 'EUR', scale: 2, count: 1, first: '2026-03-01', latest: '2026-09-30' }

function projection(
  currencies: ViewProjection['currencies'],
  currency = { currency: 'USD', scale: 2 },
): ViewProjection {
  return testProjection({ currencies, view: { ...testProjection().view, currency } })
}

describe('CurrencyChip', () => {
  beforeAll(() => {
    // jsdom has no layout; kit SelectDropdown scrolls the selected option into view on open.
    Element.prototype.scrollIntoView ??= () => undefined
  })
  afterEach(cleanup)

  it('names the view currency and lists each currency with its transaction count', async () => {
    render(CurrencyChip, { projection: projection([usd, eur]), onchange: vi.fn() })
    const trigger = screen.getByRole('combobox', { name: 'Currency: USD' })
    await fireEvent.click(trigger)
    expect(screen.getAllByRole('option').map((option) => option.textContent?.trim())).toEqual([
      'USD · 1,234 transactions',
      'EUR · 1 transaction',
    ])
  })

  it('switches the view to the currency chosen', async () => {
    const onchange = vi.fn()
    render(CurrencyChip, { projection: projection([usd, eur]), onchange })
    await fireEvent.click(screen.getByRole('combobox', { name: 'Currency: USD' }))
    await fireEvent.click(screen.getByRole('option', { name: /^EUR/ }))
    expect(onchange).toHaveBeenCalledWith({ currency: 'EUR', scale: 2 })
  })

  it('stays available when the last transaction in the view currency is gone', async () => {
    const onchange = vi.fn()
    render(CurrencyChip, { projection: projection([usd], { currency: 'EUR', scale: 2 }), onchange })
    await fireEvent.click(screen.getByRole('combobox', { name: 'Currency: EUR' }))
    expect(screen.getAllByRole('option').map((option) => option.textContent?.trim())).toEqual([
      'USD · 1,234 transactions',
      'EUR · 0 transactions',
    ])
    await fireEvent.click(screen.getByRole('option', { name: /^USD/ }))
    expect(onchange).toHaveBeenCalledWith({ currency: 'USD', scale: 2 })
  })

  it('is hidden with only one currency', () => {
    render(CurrencyChip, { projection: projection([usd]), onchange: vi.fn() })
    expect(screen.queryByRole('combobox')).toBeNull()
  })
})
