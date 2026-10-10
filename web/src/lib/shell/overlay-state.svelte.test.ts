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
    const { overlays, host } = setup()
    const actions = document.createElement('button')
    host.grid().append(actions)
    actions.focus()
    overlays.open('actions', actions)
    overlays.open('merchant')
    host.grid().focus()
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

  it('returns focus to the same control in the row now at the opener index', async () => {
    const { overlays, host } = setup()
    const row = (index: number, label: string) => {
      const element = document.createElement('div')
      element.setAttribute('role', 'row')
      element.dataset['index'] = String(index)
      element.innerHTML = `<div data-column="actions"><button>${label}</button></div>`
      host.grid().append(element)
      return element
    }
    const first = row(0, 'first')
    const second = row(1, 'second')
    first.querySelector('button')!.focus()
    overlays.open('merchant', first.querySelector('button')!)
    // The edit removed the opener's row; the next row moved up to its index.
    first.remove()
    second.dataset['index'] = '0'
    overlays.close()
    await afterRestore()
    expect(document.activeElement?.textContent).toBe('second')
    expect(host.focusGrid).not.toHaveBeenCalled()
  })

  it('clears the anchor on close', () => {
    const { overlays, opener } = setup()
    overlays.anchor = opener
    overlays.open('merchant')
    overlays.close()
    expect(overlays.anchor).toBeUndefined()
  })
})

describe('OverlayState escape layers', () => {
  afterEach(() => document.body.replaceChildren())

  /** A panel holding a button, with an escape layer attached. */
  function layer(overlays: OverlayState, onescape: () => void) {
    const panel = document.createElement('div')
    const button = document.createElement('button')
    panel.append(button)
    document.body.append(panel)
    const detach = overlays.escapeLayer(onescape)(panel)
    return { button, detach: () => detach?.() }
  }

  function escape(target: HTMLElement): KeyboardEvent {
    const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    target.dispatchEvent(event)
    return event
  }

  it('lets only the innermost layer take Escape', () => {
    const { overlays } = setup()
    const outer = vi.fn()
    const inner = vi.fn()
    const outerLayer = layer(overlays, outer)
    const innerLayer = layer(overlays, inner)

    expect(escape(outerLayer.button).defaultPrevented).toBe(false)
    expect(outer).not.toHaveBeenCalled()
    expect(escape(innerLayer.button).defaultPrevented).toBe(true)
    expect(inner).toHaveBeenCalledTimes(1)

    innerLayer.detach()
    escape(outerLayer.button)
    expect(outer).toHaveBeenCalledTimes(1)
  })

  it('leaves Escape to a claimed key or an open popup inside the panel', () => {
    const { overlays } = setup()
    const onescape = vi.fn()
    const { button } = layer(overlays, onescape)
    button.addEventListener('keydown', (event) => event.preventDefault())
    escape(button)
    const other = layer(overlays, onescape)
    other.button.setAttribute('aria-expanded', 'true')
    escape(other.button)
    expect(onescape).not.toHaveBeenCalled()
  })

  it('stops a taken Escape before window listeners see it', () => {
    const { overlays } = setup()
    const windowListener = vi.fn()
    window.addEventListener('keydown', windowListener)
    const { button } = layer(overlays, vi.fn())
    escape(button)
    window.removeEventListener('keydown', windowListener)
    expect(windowListener).not.toHaveBeenCalled()
  })
})
