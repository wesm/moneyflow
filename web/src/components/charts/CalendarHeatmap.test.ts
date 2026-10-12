import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, onTestFinished, vi } from 'vitest'

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

  it('shades Out by level, dots the days with In, and shows each day number', () => {
    const { day } = setup()
    const level = (name: string) => day(name).style.getPropertyValue('--mf-level')
    expect(level('Oct 5, 2026')).toBe('4')
    expect(day('Oct 5, 2026').className).toMatch(/mf-day--in/)
    expect(level('Oct 2, 2026')).toBe('2')
    expect(day('Oct 2, 2026').className).not.toMatch(/mf-day--in/)
    expect(level('Oct 3, 2026')).toBe('0')
    expect(day('Oct 3, 2026').textContent?.trim()).toBe('3')
  })

  it('heads the month grid with Sunday-first weekday initials and keys the levels', () => {
    const { group } = setup()
    const initials = [...group.querySelectorAll('.mf-calendar__weekday')].map(
      (cell) => cell.textContent,
    )
    expect(initials).toEqual(['S', 'M', 'T', 'W', 'T', 'F', 'S'])
    expect(screen.getByText('Less')).not.toBeNull()
    expect(screen.getByText('More')).not.toBeNull()
  })

  it('shows a dash for a day without Out in the tooltip, keeping +0.00 in its name', async () => {
    const { day } = setup()
    const third = day('Oct 3, 2026')
    expect(third.getAttribute('aria-label')).toBe('Oct 3, 2026, out +0.00')
    third.focus()
    const tip = await screen.findByRole('tooltip')
    expect(tip.textContent).toContain('Oct 3, 2026')
    expect(tip.textContent).toContain('—')
    expect(tip.textContent).not.toContain('+0.00')
  })

  it('mounts one tooltip, for the day under the pointer or with focus', async () => {
    const { group, day } = setup()
    const triggers = () => group.querySelectorAll('.kit-tooltip-trigger')
    // At rest only the Tab stop carries one, so Tab lands on a day that is not redrawn.
    expect(triggers()).toHaveLength(1)
    expect(triggers()[0]!.contains(day('Oct 5, 2026'))).toBe(true)

    await fireEvent.pointerOver(day('Oct 2, 2026'))
    await waitFor(() => expect(triggers()[0]!.contains(day('Oct 2, 2026'))).toBe(true))
    expect(triggers()).toHaveLength(1)
    await fireEvent.mouseEnter(triggers()[0]!)
    const hovered = await screen.findByRole('tooltip')
    expect(hovered.textContent).toContain('Oct 2, 2026')
    expect(hovered.textContent).toContain('-300.00')

    day('Oct 4, 2026').focus()
    expect(document.activeElement).toBe(day('Oct 4, 2026'))
    expect(triggers()).toHaveLength(1)
    expect(triggers()[0]!.contains(day('Oct 4, 2026'))).toBe(true)
    const focused = await screen.findByRole('tooltip')
    expect(focused.textContent).toContain('Oct 4, 2026')
    expect(screen.getAllByRole('tooltip')).toHaveLength(1)
  })

  it('keeps focus on a keyboard-focused day when the pointer moves to another', async () => {
    const { group, day } = setup()
    day('Oct 4, 2026').focus()
    await fireEvent.pointerOver(day('Oct 2, 2026'))
    await waitFor(() =>
      expect(group.querySelector('.kit-tooltip-trigger')!.contains(day('Oct 2, 2026'))).toBe(true),
    )
    expect(document.activeElement).toBe(day('Oct 4, 2026'))
  })

  it('keeps a pressed day’s button until its click, then gives it the tooltip', async () => {
    const { onperiod, group, day } = setup()
    const pressed = day('Oct 2, 2026')
    await fireEvent.pointerOver(pressed)
    await fireEvent.pointerDown(pressed)
    pressed.focus()
    await new Promise((resolve) => setTimeout(resolve))
    expect(day('Oct 2, 2026')).toBe(pressed)
    await fireEvent.pointerUp(pressed)
    await fireEvent.click(pressed)
    expect(onperiod).toHaveBeenCalledWith({ start: '2026-10-02', end: '2026-10-02' })
    await waitFor(() =>
      expect(group.querySelector('.kit-tooltip-trigger')!.contains(day('Oct 2, 2026'))).toBe(true),
    )
    expect(document.activeElement).toBe(day('Oct 2, 2026'))
  })

  it('names every day without a tooltip mounted', () => {
    const { group } = setup()
    const named = [...group.querySelectorAll('button.mf-day')].filter((button) =>
      /^Oct \d+, 2026, out /.test(button.getAttribute('aria-label') ?? ''),
    )
    expect(named).toHaveLength(7)
  })

  it('outlines the scoped day of a Day scope', () => {
    const { group, day } = setup()
    expect(day('Oct 5, 2026').className).toMatch(/mf-day--scope/)
    expect(group.querySelectorAll('.mf-day--scope')).toHaveLength(1)
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

  it('labels each month grid, and labels the strip with month ticks and weekdays', () => {
    setup()
    expect(screen.getByText('Oct 2026')).not.toBeNull()
    cleanup()
    const { group } = setup({}, october('weeks'))
    expect(group.classList.contains('mf-calendar--weeks')).toBe(true)
    expect(screen.queryByText('Oct 2026')).toBeNull()
    expect(within(group).getByText('Oct')).not.toBeNull()
    const rows = [...group.querySelectorAll('.mf-calendar__weekday')].map(
      (cell) => cell.textContent,
    )
    expect(rows).toEqual(['', 'M', '', 'W', '', 'F', ''])
  })

  it('keeps watching the strip’s month names while the cursor moves', async () => {
    const observe = vi.fn()
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe = observe
        unobserve(): void {}
        disconnect(): void {}
      },
    )
    onTestFinished(() => void vi.unstubAllGlobals())
    const onperiod = vi.fn()
    const props = { entry: october('weeks'), onperiod, cursorDate: '2026-10-02' }
    const { rerender } = render(CalendarHeatmap, props)
    expect(observe).toHaveBeenCalledTimes(1)

    await rerender({ ...props, cursorDate: '2026-10-05' })
    expect(observe).toHaveBeenCalledTimes(1)
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
