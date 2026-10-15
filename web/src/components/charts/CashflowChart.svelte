<script lang="ts">
  import CashflowColumn from './CashflowColumn.svelte'
  import {
    cashflowGeometry,
    cursorGeometry,
    NET_DOT_RADIUS,
    rangeAnchor,
    rowAmounts,
    scopeFromColumns,
    tickEvery,
    type CashflowCursor,
  } from '../../lib/charts/cashflow'
  import type { InsightsEntry } from '../../lib/insights/cache.svelte'
  import type { Period } from '../../lib/shell/period'

  interface Props {
    entry: InsightsEntry
    /** What to draw solid: the cursor row's series, or the cursor period's column. */
    cursor?: CashflowCursor | undefined
    /** Scopes the view to a column or to a range of them. */
    onperiod: (period: Period) => void
    /** Esc leaves the chart for the table; without it, Esc is left to the enclosing dialog. */
    onleave?: (() => void) | undefined
  }

  let { entry, cursor, onperiod, onleave }: Props = $props()
  /** Room under the bars for the axis labels. */
  const tickHeight = 16
  const moves = new Set(['ArrowLeft', 'ArrowRight', 'Home', 'End'])
  let plot = $state<HTMLElement>()
  let width = $state(0)
  let height = $state(0)
  const drawn = $derived(Math.max(height - tickHeight, 0))
  const geometry = $derived(cashflowGeometry(entry, width, drawn))
  const columns = $derived(geometry.columns)
  const bars = $derived(cursor && cursorGeometry(entry, cursor, drawn))
  const every = $derived(
    tickEvery(columns.length, width, entry.response.ranges.context_granularity),
  )
  const context = $derived(entry.response.ranges.context)
  /** The period that holds the Tab stop, while the same view is drawn. */
  let chosen = $state<{ query: string; period: string }>()
  const scoped = $derived(columns.findLastIndex((column) => column.inScope))
  // A new revision of the same view can redraw other periods, such as years instead of months.
  const kept = $derived(
    chosen?.query === entry.query
      ? context.findIndex((period) => periodKey(period) === chosen?.period)
      : -1,
  )
  const active = $derived(kept >= 0 ? kept : scoped < 0 ? columns.length - 1 : scoped)
  let drag = $state<{ from: number; to: number }>()
  /** The Tab stop when the pointer went down, before that press focused the column. */
  let pressedFrom: number | undefined

  function periodKey(period: { start: string; end: string }): string {
    return `${period.start}/${period.end}`
  }

  function hold(index: number): void {
    const period = context[index]
    if (period) chosen = { query: entry.query, period: periodKey(period) }
  }

  function dragged(index: number): boolean {
    if (drag === undefined) return false
    return index >= Math.min(drag.from, drag.to) && index <= Math.max(drag.from, drag.to)
  }

  /**
   * Scopes to one column, or with `from` set, from the scope's far edge through it. Without a
   * scope in the chart, the range runs from the `from` column instead.
   */
  function choose(index: number, from: number | undefined): void {
    const anchor = from === undefined ? index : rangeAnchor(columns, index, from)
    hold(index)
    onperiod(scopeFromColumns(context, anchor, index))
  }

  function click(event: MouseEvent, index: number): void {
    const from = pressedFrom ?? active
    pressedFrom = undefined
    choose(index, event.shiftKey ? from : undefined)
  }

  function keydown(event: KeyboardEvent, index: number): void {
    pressedFrom = undefined
    if (event.key === 'Escape' && onleave) {
      event.preventDefault()
      onleave()
      return
    }
    // Enter alone activates the button; Shift+Enter extends the range, as a shift-click does.
    if (event.key === 'Enter' && event.shiftKey) {
      event.preventDefault()
      choose(index, active)
      return
    }
    if (!moves.has(event.key)) return
    event.preventDefault()
    const last = columns.length - 1
    const step = { ArrowLeft: index - 1, ArrowRight: index + 1, Home: 0, End: last }
    const next = Math.min(Math.max(step[event.key as keyof typeof step], 0), last)
    hold(next)
    plot?.querySelectorAll<HTMLElement>('.mf-cashflow__column')[next]?.focus()
  }

  function press(event: PointerEvent, index: number): void {
    if (event.button !== 0) return
    pressedFrom = active
    if (event.shiftKey) return
    // A touch captures the pointer to the element it pressed, such as the column's bars;
    // releasing it lets the drag reach the other columns.
    const target = event.target
    if (target instanceof Element && target.hasPointerCapture?.(event.pointerId)) {
      target.releasePointerCapture(event.pointerId)
    }
    drag = { from: index, to: index }
  }

  /** Ends a drag; a drag across columns sets their range, and a press in place stays a click. */
  function release(): void {
    const done = drag
    drag = undefined
    if (done && done.from !== done.to) onperiod(scopeFromColumns(context, done.from, done.to))
  }
</script>

<svelte:window onpointerup={release} onpointercancel={() => (drag = undefined)} />

<div class="mf-cashflow" data-cursor={bars && cursor ? cursor.identity : undefined}>
  <div
    class="mf-cashflow__plot"
    role="group"
    aria-label="Cash flow"
    style:grid-template-columns={`repeat(${columns.length}, minmax(0, 1fr))`}
    bind:this={plot}
    bind:clientWidth={width}
    bind:clientHeight={height}
  >
    {#each columns as column, index (index)}
      <CashflowColumn
        {column}
        zeroY={geometry.zeroY}
        height={drawn}
        cursor={bars?.[index]}
        row={rowAmounts(cursor, index, entry.response.scale)}
        ticked={column.tickOrder % every === 0}
        active={index === active}
        dragged={dragged(index)}
        onclick={(event) => click(event, index)}
        onkeydown={(event) => keydown(event, index)}
        onpointerdown={(event) => press(event, index)}
        onpointerenter={() => drag && (drag = { ...drag, to: index })}
        onfocus={() => hold(index)}
      />
    {/each}
    {#if width > 0}
      <svg class="mf-cashflow__lines" {width} height={drawn} aria-hidden="true">
        <line class="mf-cashflow__zero" x2={width} y1={geometry.zeroY} y2={geometry.zeroY} />
        <path class="mf-cashflow__net" d={geometry.netPath} />
        {#each columns as column, index (index)}
          <circle
            class="mf-cashflow__dot"
            cx={column.x + column.width / 2}
            cy={column.netY}
            r={NET_DOT_RADIUS}
          />
        {/each}
      </svg>
    {/if}
  </div>
</div>
