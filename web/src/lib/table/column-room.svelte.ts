/** The width inside `element`'s padding, in rem. */
function contentRem(element: HTMLElement): number {
  const style = getComputedStyle(element)
  const padding = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight)
  const rem = parseFloat(getComputedStyle(document.documentElement).fontSize)
  return (element.clientWidth - padding) / rem
}

/**
 * The width the grid's columns may fill, measured on the scroller that holds it, so the table
 * drops columns to fit instead of scrolling sideways. `rem` is undefined until the scroller
 * exists. Create it while a component initializes.
 */
export class ColumnRoom {
  #width = $state<number>()

  constructor(scroller: () => HTMLElement | undefined) {
    // Before the grid updates, so the first drawn columns already fit.
    $effect.pre(() => {
      const element = scroller()
      if (!element) return
      this.#width = contentRem(element)
      const observer = new ResizeObserver(() => (this.#width = contentRem(element)))
      observer.observe(element)
      return () => observer.disconnect()
    })
  }

  get rem(): number | undefined {
    return this.#width
  }
}
