import { getContext, setContext } from 'svelte'

/** The context key; tests mount a component with `new Map([[revealHoldKey, hold]])`. */
export const revealHoldKey = Symbol('reveal-hold')

/**
 * Delays scrolling the table to the cursor row while the cursor itself moves at once. The chart
 * and the table share one scroller, so a click on a chart bar that revealed its row at once
 * would carry the bar away before a double-click's second click reached it.
 */
export class RevealHold {
  /** Whether cursor moves wait to be revealed. */
  held = $state(false)
  #timer: ReturnType<typeof setTimeout> | undefined
  #deferred: (() => void) | undefined

  /** Holds reveals for `millis`, then reveals the cursor row if a move waited. */
  start(millis: number): void {
    clearTimeout(this.#timer)
    this.held = true
    this.#timer = setTimeout(() => this.end(true), millis)
  }

  /** Ends the hold; with `reveal` false, a move that waited stays where it is. */
  end(reveal: boolean): void {
    clearTimeout(this.#timer)
    this.#timer = undefined
    const deferred = this.#deferred
    this.#deferred = undefined
    this.held = false
    if (reveal) deferred?.()
  }

  /** Records the reveal to run when the hold ends. */
  defer(reveal: () => void): void {
    this.#deferred = reveal
  }
}

/** Shares `hold` with the table and charts below the calling component. */
export function provideRevealHold(hold: RevealHold): void {
  setContext(revealHoldKey, hold)
}

/** The hold an ancestor provided, if any. Call during component initialization. */
export function revealHold(): RevealHold | undefined {
  return getContext<RevealHold | undefined>(revealHoldKey)
}
