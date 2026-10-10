import { MediaQuery } from 'svelte/reactivity'

import { FIT_COLUMNS_QUERY } from './columns'

/** The width inside `element`'s padding, in rem. */
function contentRem(element: HTMLElement): number {
  const style = getComputedStyle(element)
  const padding = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight)
  const rem = parseFloat(getComputedStyle(document.documentElement).fontSize)
  return (element.clientWidth - padding) / rem
}

/**
 * The width the grid's columns may fill, measured on the scroller that holds it. From 900 px
 * the table drops columns to fit it; below that it scrolls sideways, as the spec allows, so
 * `rem` is undefined there. Create it while a component initializes.
 */
export class ColumnRoom {
  readonly #fits = new MediaQuery(FIT_COLUMNS_QUERY)
  #width = $state<number>()

  constructor(scroller: () => HTMLElement | undefined) {
    $effect(() => {
      const element = scroller()
      // Below 900 px the table scrolls instead, so nothing needs measuring.
      if (!element || !this.#fits.current) return
      this.#width = contentRem(element)
      const observer = new ResizeObserver(() => (this.#width = contentRem(element)))
      observer.observe(element)
      return () => observer.disconnect()
    })
  }

  get rem(): number | undefined {
    return this.#fits.current ? this.#width : undefined
  }
}
