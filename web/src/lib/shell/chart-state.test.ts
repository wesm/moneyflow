import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ChartState } from './chart-state.svelte'

function chartState(width: { compact: boolean }) {
  const pop = vi.fn()
  const pushScope = vi.fn<(name: string) => () => void>(() => pop)
  const charts = new ChartState({ compact: () => width.compact, pushScope })
  return { charts, pushScope, pop }
}

/** A browser storage that keeps values in memory, as jsdom's test origin has none. */
function memoryStorage(): Pick<Storage, 'getItem' | 'setItem'> {
  const values = new Map<string, string>()
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => void values.set(key, value),
  }
}

describe('ChartState', () => {
  beforeEach(() => vi.stubGlobal('localStorage', memoryStorage()))
  afterEach(() => vi.unstubAllGlobals())

  it('starts the chart row on above phone width and hides it with the switch', () => {
    const { charts } = chartState({ compact: false })
    expect(charts.row).toBe(true)
    expect(charts.checked).toBe(true)

    charts.toggle(false)
    expect(charts.row).toBe(false)
    expect(charts.checked).toBe(false)
    charts.toggle(true)
    expect(charts.row).toBe(true)
  })

  it('remembers the choice in this browser', () => {
    chartState({ compact: false }).charts.toggle(false)
    expect(chartState({ compact: false }).charts.row).toBe(false)

    chartState({ compact: false }).charts.toggle(true)
    expect(chartState({ compact: false }).charts.row).toBe(true)
  })

  it('works without browser storage', () => {
    const broken = {
      getItem: () => {
        throw new Error('storage is disabled')
      },
      setItem: () => {
        throw new Error('storage is disabled')
      },
    }
    vi.stubGlobal('localStorage', broken)
    const { charts } = chartState({ compact: false })
    expect(charts.row).toBe(true)
    charts.toggle(false)
    expect(charts.row).toBe(false)
  })

  it('opens a dialog with its own shortcut scope at phone width', () => {
    const width = { compact: true }
    const { charts, pushScope, pop } = chartState(width)
    expect(charts.row).toBe(false)
    expect(charts.checked).toBe(false)
    charts.toggle(true)
    expect(charts.row).toBe(false)
    expect(charts.dialogOpen).toBe(true)
    expect(charts.checked).toBe(true)
    expect(pushScope).toHaveBeenCalledWith('charts')

    charts.toggle(false)
    expect(charts.dialogOpen).toBe(false)
    expect(charts.checked).toBe(false)
    expect(pop).toHaveBeenCalledTimes(1)
  })

  it('leaves the chart row on after the phone dialog opens and closes', () => {
    const width = { compact: true }
    const { charts } = chartState(width)
    charts.toggle(true)
    charts.toggle(false)
    width.compact = false
    expect(charts.row).toBe(true)
  })

  it('keeps the chart row off after the phone dialog opens', () => {
    const width = { compact: false }
    const { charts } = chartState(width)
    charts.toggle(false)
    width.compact = true
    charts.toggle(true)
    width.compact = false
    expect(charts.row).toBe(false)
  })

  it('pushes the open dialog scope onto a rebuilt shortcut manager and pops it on destroy', () => {
    const { charts, pushScope, pop } = chartState({ compact: true })
    charts.rescope()
    expect(pushScope).not.toHaveBeenCalled()
    charts.setDialog(true)
    charts.rescope()
    expect(pushScope).toHaveBeenCalledTimes(2)
    charts.destroy()
    expect(pop).toHaveBeenCalledTimes(1)
  })
})
