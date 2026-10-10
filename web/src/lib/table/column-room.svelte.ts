import { innerWidth } from 'svelte/reactivity/window'

import { FIT_COLUMNS_WIDTH } from './columns'

/** The width inside `element`'s padding, in rem. */
function contentRem(element: HTMLElement): number {
  const style = getComputedStyle(element)
  const padding = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight)
  const rem = parseFloat(getComputedStyle(document.documentElement).fontSize)
  return (element.clientWidth - padding) / rem
}

function fits(): boolean {
  return (innerWidth.current ?? 0) >= FIT_COLUMNS_WIDTH
}

/**
 * The width the grid's columns may fill, measured on the scroller that holds it. From 900 px
 * the table drops columns to fit it; below that it scrolls sideways, as the spec allows, so
 * `rem` is undefined there. Create it while a component initializes.
 */
export class ColumnRoom {
  #width = $state<number>()

  constructor(scroller: () => HTMLElement | undefined) {
    // Before the grid updates, and on the resize event, which runs before media query listeners
    // such as the one that leaves phone rows. A width last measured elsewhere is never drawn.
    $effect.pre(() => {
      const element = scroller()
      // Below 900 px the table scrolls instead, so nothing needs measuring.
      if (!element || !fits()) return
      this.#width = contentRem(element)
      const observer = new ResizeObserver(() => (this.#width = contentRem(element)))
      observer.observe(element)
      return () => observer.disconnect()
    })
  }

  get rem(): number | undefined {
    return fits() ? this.#width : undefined
  }
}
