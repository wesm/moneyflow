import { describe, expect, it, vi } from 'vitest'

import { ChartState } from './chart-state.svelte'

function chartState(width: { full: boolean; compact: boolean }) {
  const pop = vi.fn()
  const pushScope = vi.fn<(name: string) => () => void>(() => pop)
  const charts = new ChartState({
    fullSidebar: () => width.full,
    compact: () => width.compact,
    pushScope,
  })
  return { charts, pushScope, pop }
}

describe('ChartState', () => {
  it('follows the width until the user flips the switch', () => {
    const width = { full: true, compact: false }
    const { charts } = chartState(width)
    expect(charts.aside).toBe(true)
    expect(charts.checked).toBe(true)

    width.full = false
    expect(charts.aside).toBe(false)
    expect(charts.checked).toBe(false)

    charts.toggle(true)
    expect(charts.aside).toBe(true)
    width.full = true
    charts.toggle(false)
    expect(charts.aside).toBe(false)
    width.full = false
    expect(charts.checked).toBe(false)
  })

  it('opens a dialog with its own shortcut scope at phone width', () => {
    const width = { full: false, compact: true }
    const { charts, pushScope, pop } = chartState(width)
    charts.toggle(true)
    expect(charts.aside).toBe(false)
    expect(charts.dialogOpen).toBe(true)
    expect(charts.checked).toBe(true)
    expect(pushScope).toHaveBeenCalledWith('charts')

    charts.toggle(false)
    expect(charts.dialogOpen).toBe(false)
    expect(charts.checked).toBe(false)
    expect(pop).toHaveBeenCalledTimes(1)
  })

  it('keeps the phone switch apart from the wider-width choice', () => {
    const width = { full: false, compact: true }
    const { charts } = chartState(width)
    charts.toggle(true)
    width.compact = false
    expect(charts.aside).toBe(false)
    expect(charts.checked).toBe(false)
  })

  it('pushes the open dialog scope onto a rebuilt shortcut manager and pops it on destroy', () => {
    const { charts, pushScope, pop } = chartState({ full: false, compact: true })
    charts.rescope()
    expect(pushScope).not.toHaveBeenCalled()
    charts.setDialog(true)
    charts.rescope()
    expect(pushScope).toHaveBeenCalledTimes(2)
    charts.destroy()
    expect(pop).toHaveBeenCalledTimes(1)
  })
})
