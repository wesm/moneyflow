import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'

import DetailSummary from './DetailSummary.svelte'
import type { ViewProjection } from '../lib/api/client'

describe('DetailSummary', () => {
  afterEach(cleanup)

  it('renders exact income, outflow, and net cards per money partition', () => {
    render(DetailSummary, { summary: [statistics()] })
    expect(screen.getByRole('region', { name: 'USD summary' })).not.toBeNull()
    expect(screen.getByText('+$100.00')).not.toBeNull()
    expect(screen.getByText('-$25.00')).not.toBeNull()
    expect(screen.getByText('+$75.00')).not.toBeNull()
  })
})

function statistics(): NonNullable<ViewProjection['chart']['summary']>[number] {
  return {
    currency: 'USD',
    scale: 2,
    count: 3,
    in: money('10000', '100.00', '+$100.00'),
    out: money('-2500', '-25.00', '-$25.00'),
    net: money('7500', '75.00', '+$75.00'),
  }
}

function money(minor: string, decimal: string, display: string) {
  return { minor, currency: 'USD', scale: 2, decimal, display }
}
