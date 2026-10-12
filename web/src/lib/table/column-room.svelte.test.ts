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

  it('measures the scroller at every width and keeps one observer while the window resizes', () => {
    const observe = vi.fn()
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe = observe
        unobserve(): void {}
        disconnect(): void {}
      },
    )
    resizeTo(700)
    const scroller = document.createElement('div')
    Object.defineProperty(scroller, 'clientWidth', { configurable: true, value: 640 })
    scroller.style.padding = '0px'
    document.documentElement.style.fontSize = '16px'
    let room: ColumnRoom | undefined
    const stop = $effect.root(() => {
      room = new ColumnRoom(() => scroller)
      $effect(() => void room?.rem)
    })
    flushSync()
    // 640 px at a 16 px root font size, at a window narrower than 900 px.
    expect(room?.rem).toBe(40)
    expect(observe).toHaveBeenCalledTimes(1)

    for (const width of [1100, 820, 641, 1000]) resizeTo(width)
    expect(observe).toHaveBeenCalledTimes(1)
    stop()
    document.documentElement.style.fontSize = ''
  })
})
