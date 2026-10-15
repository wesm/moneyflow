// Browser APIs jsdom lacks. Setup imports this module before any other, because a module such
// as kit-ui may call these while it loads, before the rest of setup runs.

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

// jsdom lays nothing out, so it has no scrollIntoView; scrolling into view does nothing here.
Element.prototype.scrollIntoView ??= () => undefined

// The window is 700 px wide, below the widths that add or fit table columns, unless a test sets
// another width.
Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 700 })

// jsdom draws nothing and warns on every getContext call; with no context, code that measures
// text on a canvas falls back as it would in a browser without one.
HTMLCanvasElement.prototype.getContext = () => null
