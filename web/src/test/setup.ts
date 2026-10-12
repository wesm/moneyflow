import { beforeEach } from 'vitest'

import { memoryStorage } from './storage'

if (!window.matchMedia) {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
    }),
  })
}

class TestResizeObserver implements ResizeObserver {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

Object.defineProperty(globalThis, 'ResizeObserver', {
  configurable: true,
  value: TestResizeObserver,
})

// The window is 700 px wide, below the widths that add or fit table columns, unless a test sets
// another width.
Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 700 })

// Each test starts with empty storage. Defining the globals without reading them first avoids
// Node's built-in Web Storage, whose getter warns when no storage file is configured.
beforeEach(() => {
  for (const name of ['localStorage', 'sessionStorage']) {
    Object.defineProperty(globalThis, name, { configurable: true, value: memoryStorage() })
  }
})
