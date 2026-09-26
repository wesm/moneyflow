import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import AggregateBars from './AggregateBars.svelte'
import type { ChartPartition } from '../lib/chart'

describe('AggregateBars', () => {
  afterEach(cleanup)

  it('renders labelled horizontal partitions linked to cursor and drill behavior', async () => {
    const oncursor = vi.fn()
    const ondrill = vi.fn()
    render(AggregateBars, { partitions: [partition()], cursorIndex: 1, oncursor, ondrill })
    expect(screen.getByRole('region', { name: 'USD chart' })).not.toBeNull()
    const mark = screen.getByRole('button', { name: 'Coffee, -$12.34' })
    expect(mark.getAttribute('aria-pressed')).toBe('true')
    expect(mark.classList.contains('chart-mark--active')).toBe(true)
    await fireEvent.click(mark)
    expect(oncursor).toHaveBeenCalledWith(1)
    const bubbled = vi.fn()
    window.addEventListener('keydown', bubbled, { once: true })
    await fireEvent.keyDown(mark, { key: 'Enter', bubbles: true })
    expect(bubbled).not.toHaveBeenCalled()
    await fireEvent.doubleClick(mark)
    expect(ondrill).toHaveBeenCalledTimes(2)
    expect(ondrill).toHaveBeenCalledWith('coffee')
  })
})

function partition(): ChartPartition {
  return {
    key: 'USD:2',
    currency: 'USD',
    scale: 2,
    marks: [
      {
        identity: 'coffee',
        categoricalKey: 'coffee',
        index: 1,
        label: 'Coffee',
        display: '-$12.34',
        ratio: -8000,
        currency: 'USD',
        scale: 2,
      },
    ],
  }
}
