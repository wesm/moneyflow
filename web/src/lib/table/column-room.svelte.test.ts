import { flushSync } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ColumnRoom } from './column-room.svelte'

function resizeTo(width: number): void {
  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: width })
  window.dispatchEvent(new Event('resize'))
  flushSync()
}

describe('ColumnRoom', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    resizeTo(700)
  })

  it('keeps one observer while resizes stay on one side of 900 px', () => {
    const observe = vi.fn()
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe = observe
        unobserve(): void {}
        disconnect(): void {}
      },
    )
    resizeTo(1000)
    const scroller = document.createElement('div')
    const stop = $effect.root(() => {
      const room = new ColumnRoom(() => scroller)
      $effect(() => void room.rem)
    })
    flushSync()
    expect(observe).toHaveBeenCalledTimes(1)

    resizeTo(1100)
    resizeTo(950)
    expect(observe).toHaveBeenCalledTimes(1)

    resizeTo(800)
    resizeTo(1000)
    expect(observe).toHaveBeenCalledTimes(2)
    stop()
  })
})
