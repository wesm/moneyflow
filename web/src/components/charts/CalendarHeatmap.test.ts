import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import CalendarHeatmap from './CalendarHeatmap.svelte'
import type { InsightsResponse } from '../../lib/api/client'
import type { InsightsEntry } from '../../lib/insights/cache.svelte'
import { testInsights } from '../../test/insights'

type Heatmap = NonNullable<InsightsResponse['ranges']['heatmap']>

/** October 2026 scoped to Oct 5, with today the 7th: Out on the 2nd, In and Out on the 5th. */
function october(layout: Heatmap['layout'] = 'months'): InsightsEntry {
  const response = testInsights()
  response.ranges = {
    ...response.ranges,
    kind: 'day',
    scope: { start: '2026-10-05', end: '2026-10-05' },
    heatmap: { layout, start: '2026-10-01', end: '2026-10-31' },
  }
  const days = Array.from({ length: 31 }, (_, index) => index + 1)
  response.summary!.calendar = {
    in: days.map((day) => (day === 5 ? '12000' : '0')),
    out: days.map((day) => (day === 2 ? '-30000' : day === 5 ? '-48000' : '0')),
  }
  return { query: response.query, revision: response.revision, response, series: new Map() }
}

function setup(
  props: Partial<{ cursorDate: string; onleave: () => void }> = {},
  entry = october(),
) {
  const onperiod = vi.fn()
  render(CalendarHeatmap, { entry, onperiod, ...props })
  const group = screen.getByRole('group', { name: 'Daily spending' })
  const day = (name: string) =>
    within(group).getByRole('button', { name: new RegExp(`^${name}, `) })
  return { onperiod, group, day }
}

describe('CalendarHeatmap', () => {
  afterEach(() => cleanup())

  it('names each day with its Out, adding In only when there is some', () => {
    const { day } = setup()
    expect(day('Oct 5, 2026').getAttribute('aria-label')).toBe(
      'Oct 5, 2026, out -480.00, in +120.00',
    )
    expect(day('Oct 2, 2026').getAttribute('aria-label')).toBe('Oct 2, 2026, out -300.00')
  })

  it('shades Out by level and dots the days with In', () => {
    const { day } = setup()
    expect(day('Oct 5, 2026').className).toMatch(/mf-day--l4/)
    expect(day('Oct 5, 2026').className).toMatch(/mf-day--in/)
    expect(day('Oct 2, 2026').className).toMatch(/mf-day--l2/)
    expect(day('Oct 2, 2026').className).not.toMatch(/mf-day--in/)
    expect(day('Oct 3, 2026').className).toMatch(/mf-day--l0/)
  })

  it('fades days outside the scope and leaves future days blank and out of reach', () => {
    const { group, day } = setup()
    expect(day('Oct 5, 2026').className).not.toMatch(/mf-day--outside/)
    expect(day('Oct 6, 2026').className).toMatch(/mf-day--outside/)
    expect(within(group).queryByRole('button', { name: /^Oct 8, 2026/ })).toBeNull()
    expect(within(group).getAllByRole('button')).toHaveLength(7)
    expect(group.querySelectorAll('.mf-day--future')).toHaveLength(24)
  })

  it('outlines the cursor transaction’s day', () => {
    const { group, day } = setup({ cursorDate: '2026-10-02' })
    expect(day('Oct 2, 2026').className).toMatch(/mf-day--cursor/)
    expect(group.querySelectorAll('.mf-day--cursor')).toHaveLength(1)
  })

  it('scopes the view to a clicked day', async () => {
    const { onperiod, day } = setup()
    await fireEvent.click(day('Oct 2, 2026'))
    expect(onperiod).toHaveBeenCalledWith({ start: '2026-10-02', end: '2026-10-02' })
  })

  it('labels each month grid, and draws the strip as week columns', () => {
    setup()
    expect(screen.getByText('Oct 2026')).not.toBeNull()
    cleanup()
    const { group } = setup({}, october('weeks'))
    expect(group.classList.contains('mf-calendar--weeks')).toBe(true)
    expect(screen.queryByText('Oct 2026')).toBeNull()
  })

  it('is one Tab stop that the arrows move by day and week, and Esc leaves', async () => {
    const onleave = vi.fn()
    const { group, day } = setup({ onleave })
    expect(group.querySelectorAll('button[tabindex="0"]')).toHaveLength(1)
    // The Tab stop starts on the scope's last day.
    const fifth = day('Oct 5, 2026')
    expect(fifth.getAttribute('tabindex')).toBe('0')
    fifth.focus()
    await fireEvent.keyDown(fifth, { key: 'ArrowLeft' })
    expect(document.activeElement).toBe(day('Oct 4, 2026'))
    await fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(day('Oct 1, 2026'))
    // Future days are not reachable; the last reachable day is today.
    await fireEvent.keyDown(document.activeElement!, { key: 'End' })
    expect(document.activeElement).toBe(day('Oct 7, 2026'))
    await fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' })
    expect(document.activeElement).toBe(day('Oct 7, 2026'))
    await fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    expect(onleave).toHaveBeenCalled()
  })
})
