import { tick } from 'svelte'
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
})
