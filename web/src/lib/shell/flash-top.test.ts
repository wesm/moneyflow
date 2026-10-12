import { afterEach, describe, expect, it, vi } from 'vitest'

import { trackTop } from './flash-top'

describe('trackTop', () => {
  const original = globalThis.ResizeObserver
  afterEach(() => {
    Object.defineProperty(globalThis, 'ResizeObserver', { configurable: true, value: original })
  })

  it('reports the element top now and again whenever it resizes', () => {
    let resize = (): void => undefined
    const disconnect = vi.fn()
    class FakeObserver {
      constructor(callback: () => void) {
        resize = callback
      }
      observe = vi.fn()
      disconnect = disconnect
    }
    Object.defineProperty(globalThis, 'ResizeObserver', {
      configurable: true,
      value: FakeObserver,
    })
    const node = document.createElement('main')
    let top = 72.4
    node.getBoundingClientRect = () => ({ top }) as DOMRect
    const report = vi.fn()

    const action = trackTop(node, report)
    expect(report).toHaveBeenLastCalledWith(72)

    top = 112
    resize()
    expect(report).toHaveBeenLastCalledWith(112)

    action.destroy()
    expect(disconnect).toHaveBeenCalledTimes(1)
  })
})
