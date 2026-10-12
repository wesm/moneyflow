import { flushSync } from 'svelte'

/** The date of the heatmap day button that holds `target`, from its `data-date`. */
export function dayOf(target: EventTarget | null): string | undefined {
  return target instanceof Element
    ? target.closest<HTMLElement>('.mf-day')?.dataset['date']
    : undefined
}

/**
 * Tracks the one heatmap day drawn inside a kit Tooltip. A Tooltip on every day made a drill
 * into a transaction view build hundreds of them. Moving the tooltip replaces the button of the
 * day it leaves and the day it reaches, so this keeps focus and clicks on the right day.
 * Construct it while the heatmap initializes; it listens for pointer releases while it lives.
 */
export class DayTip {
  /** The day under the pointer, while the pointer is over the days. */
  pointed = $state<string>()
  readonly #grid: () => HTMLElement | undefined
  /** The day that has the tooltip now. */
  readonly #tipped: () => string | undefined
  #pointing: ReturnType<typeof setTimeout> | undefined
  #pressing = false
  #restoring = false

  constructor(grid: () => HTMLElement | undefined, tipped: () => string | undefined) {
    this.#grid = grid
    this.#tipped = tipped
    $effect(() => {
      const release = (event: PointerEvent) => this.#release(event)
      window.addEventListener('pointerup', release, true)
      window.addEventListener('pointercancel', release, true)
      return () => {
        clearTimeout(this.#pointing)
        window.removeEventListener('pointerup', release, true)
        window.removeEventListener('pointercancel', release, true)
      }
    })
  }

  /** The pointer moved over `target`; a gap between days keeps the pointed day. */
  over(target: EventTarget | null): void {
    if (!this.#pressing) this.#point(dayOf(target) ?? this.pointed)
  }

  /** The pointer left the days. */
  leave(): void {
    if (!this.#pressing) this.#point(undefined)
  }

  /** A press started; no button is replaced until it ends, so its click lands. */
  press(): void {
    this.#pressing = true
    clearTimeout(this.#pointing)
  }

  /**
   * A day took focus. `choose` makes it the Tab stop. Focus from the keyboard moves the tooltip
   * to it; focus from a press leaves the tooltip where it is until the press ends.
   */
  focus(target: EventTarget | null, choose: (date: string) => void): void {
    const date = dayOf(target)
    if (date === undefined || this.#restoring) return
    if (this.#pressing) {
      this.pointed = this.#tipped()
      choose(date)
      return
    }
    this.#redraw(() => {
      choose(date)
      this.pointed = undefined
    })
  }

  /** An arrow key moves focus to `date`, which takes the tooltip first so it is not replaced. */
  move(date: string, choose: (date: string) => void): void {
    choose(date)
    this.pointed = undefined
    flushSync()
    this.#button(date)?.focus()
  }

  #release(event: PointerEvent): void {
    if (!this.#pressing) return
    this.#pressing = false
    const grid = this.#grid()
    const inside = event.target instanceof Node && grid?.contains(event.target)
    this.#point(inside ? (dayOf(event.target) ?? this.pointed) : undefined)
  }

  // A pointer event goes on to the day's old button after the handlers run, so the tooltip
  // moves once the event is done. The browser then sends the new button the mouseenter that
  // opens the tooltip.
  #point(date: string | undefined): void {
    clearTimeout(this.#pointing)
    this.#pointing = setTimeout(() => this.#redraw(() => (this.pointed = date)))
  }

  /** Applies `change` now; if that replaced the focused day's button, focus follows. */
  #redraw(change: () => void): void {
    const focused = dayOf(document.activeElement)
    change()
    flushSync()
    if (focused === undefined || dayOf(document.activeElement) === focused) return
    this.#restoring = true
    this.#button(focused)?.focus()
    this.#restoring = false
  }

  #button(date: string): HTMLElement | null | undefined {
    return this.#grid()?.querySelector<HTMLElement>(`[data-date="${date}"]`)
  }
}
