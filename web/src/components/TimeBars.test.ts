import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import TimeBars from './TimeBars.svelte'
import type { ChartPartition } from '../lib/chart'

describe('TimeBars', () => {
  afterEach(cleanup)

  it('renders chronological vertical marks with exact accessible labels', () => {
    const partition: ChartPartition = {
      key: 'USD:2',
      currency: 'USD',
      scale: 2,
      marks: [
        chartMark('jan', 0, 'Jan 2026', '-$1.00', '2026-01-00'),
        chartMark('feb', 1, 'Feb 2026', '-$2.00', '2026-02-00'),
      ],
    }
    render(TimeBars, {
      partitions: [partition],
      cursorIndex: 0,
      oncursor: vi.fn(),
      ondrill: vi.fn(),
    })
    expect(screen.getByText('Chronological spending by period.')).not.toBeNull()
    expect(
      screen.getAllByRole('button').map((button) => button.getAttribute('aria-label')),
    ).toEqual(['Jan 2026, -$1.00', 'Feb 2026, -$2.00'])
  })
})

function chartMark(
  identity: string,
  index: number,
  label: string,
  display: string,
  chronologicalKey: string,
) {
  return {
    identity,
    categoricalKey: identity,
    index,
    label,
    display,
    ratio: -5000,
    currency: 'USD',
    scale: 2,
    chronologicalKey,
  }
}
