import { virtualSlice } from '@kenn-io/kit-ui'
import { untrack } from 'svelte'

import type { ViewProjection } from '../api/client'
import { cursorTarget, setPageRows, type CursorMove } from './cursor-keys'
import { revealScrollTop, visibleRange } from './layout'
import { revealHold, type RevealHold } from './reveal-hold.svelte'
import { coveredCursor, followRow, loadedRow } from './virtual-rows'

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
  /**
   * Moves the cursor for a key or a scroll once the changes queued before it run. `decide`
   * returns the row from the cursor and scroll position it finds then, or none.
   */
  onmove(decide: () => number | undefined): void
  readonly onshow: ((index: number) => void) | undefined
}

/**
 * Virtualizes the grid's rows inside the workspace scroller and keeps the cursor row on screen:
 * the grid scrolls to a cursor that keys move, and the cursor follows a scroll the user makes.
 * Construct it while the grid component initializes; its effects live as long as the grid.
 */
export class GridScroll {
  readonly #source: GridScrollSource
  readonly #hold: RevealHold | undefined = revealHold()
  /** The row last passed to `onshow`. The cursor moving there follows the scroll, so stays. */
  #shown: number | undefined
  /** The `scrollTop` the grid started at or last set to reveal the cursor row; not followed. */
  #ownTop: number | undefined
  /** A mouse button is down on the grid, as while dragging to select text across rows. */
  #pressed = false
  /** A scroll under the held button went unfollowed, so its rows may not have loaded. */
  #skipped = false
  #lastViewport: number | undefined
  #lastRowHeight: number | undefined
  readonly #projection = $derived.by(() => this.#source.windows[0]!)
  readonly #total = $derived(this.#projection.total_rows)
  // Effects that read these rerun only when the cursor moves or the view changes, not on every
  // prop update such as a new window of the same view.
  readonly #cursor = $derived.by(() => this.#source.cursorIndex)
  readonly #query = $derived.by(() => this.#projection.canonical_query)
  #lastQuery: string | undefined
  /** The grid's top in the scroller, below the room kept for its focus ring. */
  #gridTop = $state(0)
  // The scroller's top below the grid's top: the rows' offset under the sticky header.
  readonly #listTop = $derived.by(() => this.#source.scrollTop - this.#gridTop)
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
    this.#ownTop = untrack(() => source.scrollTop)
    // First, so the effects below reveal rows against the current grid top.
    $effect(() => this.#measureGridTop())
    $effect(() => this.#followCursor())
    $effect(() => this.#keepCursorAboveCover())
    $effect(() => this.#keepCursorAcrossRowHeights())
    $effect(() => this.#followScroll())
    $effect(() => this.#watchPresses())
    $effect(() => this.#loadCursorWindow())
    // Paging keys pressed outside the grid move by the page it shows.
    $effect(() => setPageRows(this.#page))
  }

  /** The rows wholly visible below the sticky header, which a paging key moves by. */
  get #page(): number {
    return this.range.last - this.range.first + 1
  }

  /** Moves the cursor as a grid key asks. */
  key(move: CursorMove): void {
    this.#source.onmove(() => this.#keyTarget(move))
  }

  /**
   * The row a grid key moves the cursor to, or none when it stays. The move is the user's, so
   * a cursor that stays has its row revealed, as scrolling may have carried it off screen.
   */
  #keyTarget(move: CursorMove): number | undefined {
    const index = cursorTarget(move, this.#cursor, this.#total, this.#page)
    // The cursor stays, so a window that scrolling asked for must not move it when it arrives.
    if (index === this.#cursor && this.#shown !== undefined) this.#source.onshow?.(index)
    this.#shown = undefined
    if (index !== this.#cursor) return index
    this.#reveal(index)
    return undefined
  }

  // Effects run after the DOM updates, so the offset read here is current.
  #measureGridTop(): void {
    this.#gridTop = this.#source.grid?.offsetTop ?? 0
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
      if (index !== this.#shown) {
        if (this.#hold?.held) return this.#hold.defer(() => this.#reveal(this.#cursor))
        return this.#reveal(index)
      }
      // A window that arrived after more scrolling moves the cursor on to the row now on screen.
      this.#shown = undefined
      this.#follow()
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

  // One move per frame, for the latest scroll position. A scroll the grid made to reveal the
  // cursor row has nothing to follow.
  #followScroll(): (() => void) | undefined {
    const top = this.#source.scrollTop
    if (top === this.#ownTop) return undefined
    this.#ownTop = undefined
    const frame = requestAnimationFrame(() => {
      if (this.#pressed) this.#skipped = true
      else this.#follow()
    })
    return () => cancelAnimationFrame(frame)
  }

  // Dragging to select text scrolls the rows under a held button; the cursor stays meanwhile.
  // Releasing follows the scroll the drag made, so rows it reached that had not loaded load.
  // A press on the scroller's scrollbar lands outside the grid, so its scroll is followed.
  #watchPresses(): (() => void) | undefined {
    const grid = this.#source.grid
    if (!grid) return undefined
    const press = (event: MouseEvent) => (this.#pressed = event.button === 0)
    const release = () => {
      this.#pressed = false
      if (!this.#skipped) return
      this.#skipped = false
      this.#follow()
    }
    grid.addEventListener('mousedown', press)
    window.addEventListener('mouseup', release)
    return () => {
      grid.removeEventListener('mousedown', press)
      window.removeEventListener('mouseup', release)
    }
  }

  // A late response, such as a selection toggle's, can bring back an older window that no longer
  // holds the cursor row on screen. Ask for the cursor's own window so its row has data again.
  #loadCursorWindow(): void {
    const cursor = this.#cursor
    const { first, last } = this.range
    if (cursor < first || cursor > last || loadedRow(this.#source.windows, cursor)) return
    untrack(() => this.#source.onshow?.(cursor))
  }

  /**
   * Moves a cursor that is off screen to the nearest row on screen. A row no loaded window holds
   * is asked for first, and the controller moves the cursor there once it arrives.
   */
  #follow(): void {
    this.#source.onmove(() => this.#followTarget())
  }

  #followTarget(): number | undefined {
    const index = followRow(this.range, this.#cursor)
    const onshow = this.#source.onshow
    if (index === undefined) {
      // Back at the cursor: the controller drops the window still on its way.
      if (this.#shown !== undefined) onshow?.(this.#cursor)
      this.#shown = undefined
      return undefined
    }
    if (loadedRow(this.#source.windows, index)) {
      this.#shown = undefined
      return index
    }
    if (!onshow) return undefined
    this.#shown = index
    onshow(index)
    return undefined
  }

  #reveal(index: number): void {
    const source = this.#source
    // In a short window the charts scroll away above the table; bring the table back into view.
    if (source.scroller) revealInBody(source.scroller)
    const gridTop = source.grid?.offsetTop ?? 0
    const port = { scrollTop: source.scrollTop, viewport: source.viewport, gridTop }
    let next = revealScrollTop(index, { ...port, rowHeight: source.rowHeight })
    if (next === source.scrollTop) return
    if (source.scroller) {
      source.scroller.scrollTop = next
      // The browser may round or clamp it; its scroll event reports what it kept.
      next = source.scroller.scrollTop
    }
    this.#ownTop = next
    source.scrollTop = next
  }
}

/**
 * Scrolls the workspace body, which holds the banners and charts above the table, just enough to
 * show the table's scroller. Unlike `scrollIntoView`, it scrolls nothing outside the body: a host
 * page around the views keeps its scroll position.
 */
function revealInBody(scroller: HTMLElement): void {
  const body = scroller.closest<HTMLElement>('.mf-workspace-body')
  if (!body) return
  const port = body.getBoundingClientRect().top + body.clientTop
  const box = scroller.getBoundingClientRect()
  const top = box.top - port
  const bottom = box.bottom - port
  if (top < 0) body.scrollTop += top
  else if (bottom > body.clientHeight) body.scrollTop += Math.min(top, bottom - body.clientHeight)
}
