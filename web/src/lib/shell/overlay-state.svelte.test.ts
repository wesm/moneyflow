import { flushSync, tick } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { OverlayState } from './overlay-state.svelte'

function setup() {
  const grid = document.createElement('div')
  const opener = document.createElement('button')
  const elsewhere = document.createElement('button')
  document.body.append(grid, opener, elsewhere)
  const popScope = vi.fn()
  const host = { pushScope: vi.fn(() => popScope), focusGrid: vi.fn(), grid: () => grid }
  return { overlays: new OverlayState(host), host, popScope, opener, elsewhere }
}

async function afterRestore(): Promise<void> {
  await tick()
  await new Promise((resolve) => requestAnimationFrame(resolve))
}

describe('OverlayState', () => {
  afterEach(() => document.body.replaceChildren())

  it('returns focus to the opener on close', async () => {
    const { overlays, opener, elsewhere, popScope } = setup()
    opener.focus()
    overlays.open('period')
    elsewhere.focus()
    overlays.close()
    expect(popScope).toHaveBeenCalledTimes(1)
    await afterRestore()
    expect(document.activeElement).toBe(opener)
  })

  it('returns focus to a given opener inside the grid, also after a stacked overlay', async () => {
    const { overlays, host, elsewhere } = setup()
    const actions = document.createElement('button')
    host.grid().append(actions)
    actions.focus()
    overlays.open('actions', actions)
    overlays.open('merchant')
    elsewhere.focus()
    expect(document.activeElement).toBe(elsewhere)
    overlays.close()
    await afterRestore()
    expect(document.activeElement).toBe(actions)
    expect(host.focusGrid).not.toHaveBeenCalled()
  })

  it("leaves focus where it is with 'keep'", async () => {
    const { overlays, host, opener, elsewhere, popScope } = setup()
    opener.focus()
    overlays.open('period')
    elsewhere.focus()
    overlays.close('keep')
    expect(overlays.current).toBeUndefined()
    expect(popScope).toHaveBeenCalledTimes(1)
    await afterRestore()
    expect(document.activeElement).toBe(elsewhere)
    expect(host.focusGrid).not.toHaveBeenCalled()
  })

  it('opens from an effect without making the effect depend on the grid or the overlay', () => {
    let grid = $state<HTMLElement | undefined>(document.createElement('div'))
    const popScope = vi.fn()
    const overlays = new OverlayState({
      pushScope: vi.fn(() => popScope),
      focusGrid: vi.fn(),
      grid: () => grid,
    })
    document.body.append(document.createElement('button'))
    document.querySelector('button')?.focus()
    let runs = 0
    const stop = $effect.root(() => {
      $effect(() => {
        runs += 1
        overlays.open('write')
      })
    })
    flushSync()
    expect(runs).toBe(1)

    grid = document.createElement('div')
    overlays.close('keep')
    flushSync()

    expect(runs).toBe(1)
    expect(overlays.current).toBeUndefined()
    stop()
  })
})
