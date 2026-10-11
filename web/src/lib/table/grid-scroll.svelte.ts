import { virtualSlice } from '@kenn-io/kit-ui'
import { untrack } from 'svelte'

import type { ViewProjection } from '../api/client'
import { revealScrollTop, visibleRange } from './layout'
import { coveredCursor, loadedRow, rowToShow } from './virtual-rows'

/**
 * What the grid shows and where its scroller is. Pass an object with getters over the grid's
 * props, so each effect tracks only the values it reads.
 */
export interface GridScrollSource {
  readonly grid: HTMLElement | undefined
  /** The workspace element that scrolls the grid. */
  readonly scroller: HTMLElement | undefined
  /** The loaded window first, then cached neighbors. */
  readonly windows: readonly ViewProjection[]
  readonly cursorIndex: number
  readonly rowHeight: number
  /** The scroller's visible height. */
  readonly viewport: number
  /** The scroller's `scrollTop`. The model sets it, and the scroller, to reveal the cursor row. */
  scrollTop: number
  oncursor(index: number): void
  readonly onshow: ((index: number) => void) | undefined
}

/**
 * Virtualizes the grid's rows inside the workspace scroller and keeps the cursor row on screen.
 * Construct it while the grid component initializes; its effects live as long as the grid.
 */
export class GridScroll {
  readonly #source: GridScrollSource
  /** The row last passed to `onshow`. The cursor moving there follows the scroll, so stays. */
  #shown: number | undefined
  #lastViewport: number | undefined
  #lastRowHeight: number | undefined
  readonly #projection = $derived.by(() => this.#source.windows[0]!)
  readonly #total = $derived(this.#projection.total_rows)
  // Effects that read these rerun only when the cursor moves or the view changes, not on every
  // prop update such as a new window of the same view.
  readonly #cursor = $derived.by(() => this.#source.cursorIndex)
  readonly #query = $derived.by(() => this.#projection.canonical_query)
  #lastQuery: string | undefined
  // The scroller's top below the grid's top: the rows' offset under the sticky header. offsetTop
  // is read, not tracked, so content above the grid that resizes does not rerun this.
  readonly #listTop = $derived.by(
    () => this.#source.scrollTop - (this.#source.grid?.offsetTop ?? 0),
  )
  /** The first and last rows wholly visible below the sticky header. */
  readonly range = $derived.by(() => {
    const height = this.#source.rowHeight
    return visibleRange(this.#listTop, this.#source.viewport - height, height, this.#total)
  })
  readonly #slice = $derived.by(() => {
    const height = this.#source.rowHeight
    return virtualSlice({
      scrollTop: this.#listTop,
      viewport: this.#source.viewport,
      count: this.#total,
      overscan: 5,
      fixedHeight: height,
      heightOf: () => height,
    })
  })
  /** The rows to render, each with its loaded row or none for a placeholder. */
  readonly slots = $derived.by(() => {
    const { start, end } = this.#slice
    return Array.from({ length: end - start }, (_, offset) => {
      const index = start + offset
      return { index, row: loadedRow(this.#source.windows, index) }
    })
  })
  /** The cursor row's identity while it is rendered, for `aria-activedescendant`. */
  readonly activeID = $derived.by(
    () => this.slots.find((slot) => slot.index === this.#cursor)?.row?.identity,
  )
  /** The spacer heights above and below the rendered rows. */
  readonly pads = $derived.by(() => {
    const { topPad, totalHeight } = this.#slice
    const rendered = this.slots.length * this.#source.rowHeight
    return { top: topPad, bottom: Math.max(0, totalHeight - topPad - rendered) }
  })

  constructor(source: GridScrollSource) {
    this.#source = source
    $effect(() => this.#followCursor())
    $effect(() => this.#keepCursorAboveCover())
    $effect(() => this.#keepCursorAcrossRowHeights())
    $effect(() => this.#showScrolledRows())
  }

  /** A grid key moves the cursor, so the next cursor move is the user's and is revealed. */
  keyed(): void {
    this.#shown = undefined
  }

  // The grid keeps focus while the cursor moves, so the browser never scrolls the active row
  // into view. Reveal it here, as a keyboard user expects. A new view, such as a sort, can keep
  // the cursor's index; its row is revealed too.
  #followCursor(): void {
    const index = this.#cursor
    const query = this.#query
    if (!this.#source.grid) return
    untrack(() => {
      const newView = this.#lastQuery !== undefined && this.#lastQuery !== query
      this.#lastQuery = query
      if (newView) this.#shown = undefined
      if (index !== this.#shown) return this.#reveal(index)
      // A window that arrived after more scrolling moves the cursor on to the row now on top,
      // once that row is loaded. Until then the next window is asked for instead.
      this.#shown = undefined
      const { first, last } = this.range
      const onScreen = index >= first && index <= last
      if (!onScreen && loadedRow([this.#projection], first)) this.#source.oncursor(first)
    })
  }

  // A shrinking viewport (the selection bar opens) reveals the cursor row it covered. Only the
  // viewport is tracked, so scrolling the cursor row away does not pull it back.
  #keepCursorAboveCover(): void {
    const next = this.#source.viewport
    untrack(() => {
      const height = this.#source.rowHeight
      const viewport = this.#lastViewport ?? next
      const before = visibleRange(this.#listTop, viewport - height, height, this.#total)
      this.#lastViewport = next
      if (coveredCursor(before, this.range, this.#cursor)) this.#reveal(this.#cursor)
    })
  }

  // Crossing 640 px (a phone turning sideways) changes the row height under the same scroll
  // position, which would carry the cursor row off screen.
  #keepCursorAcrossRowHeights(): void {
    const height = this.#source.rowHeight
    untrack(() => {
      const changed = this.#lastRowHeight !== undefined && this.#lastRowHeight !== height
      this.#lastRowHeight = height
      if (changed) this.#reveal(this.#cursor)
    })
  }

  // One request per frame, for the latest first row, once scrolling leaves the cursor's window.
  // Scrolling back to the cursor asks for its own row, which withdraws a request on its way.
  #showScrolledRows(): (() => void) | undefined {
    const { first, last } = this.range
    const cursor = this.#cursor
    const back = this.#shown !== undefined && cursor >= first && cursor <= last
    const index = back ? cursor : rowToShow(this.range, cursor, this.#projection.window)
    const onshow = this.#source.onshow
    if (!onshow || index === undefined) return undefined
    const frame = requestAnimationFrame(() => {
      this.#shown = back ? undefined : index
      onshow(index)
    })
    return () => cancelAnimationFrame(frame)
  }

  #reveal(index: number): void {
    const source = this.#source
    const gridTop = source.grid?.offsetTop ?? 0
    const port = { scrollTop: source.scrollTop, viewport: source.viewport, gridTop }
    const next = revealScrollTop(index, { ...port, rowHeight: source.rowHeight })
    if (next === source.scrollTop) return
    source.scrollTop = next
    if (source.scroller) source.scroller.scrollTop = next
  }
}
