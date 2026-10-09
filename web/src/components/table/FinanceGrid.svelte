<script lang="ts">
  import { EmptyState, virtualSlice } from '@kenn-io/kit-ui'
  import { untrack } from 'svelte'

  import AggregateRow from './AggregateRow.svelte'
  import DetailRow from './DetailRow.svelte'
  import GridHeader from './GridHeader.svelte'
  import TimeRow from './TimeRow.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import { incomeDivider } from '../../lib/table/cells'
  import { columnsFor, type SortField } from '../../lib/table/columns'
  import { revealScrollTop, rowHeight, visibleRange } from '../../lib/table/layout'
  import { doubleClickAction, pointerHit, type RowKind } from '../../lib/table/pointer'
  import { coveredCursor, loadedRow, rowToShow } from '../../lib/table/virtual-rows'

  interface Props {
    /** The loaded window of the result. */
    projection: ViewProjection
    /** Cached windows next to it; their rows draw instead of placeholders. */
    neighbors?: readonly ViewProjection[]
    cursorIndex: number
    /** Two-line phone rows at 640 px and narrower. */
    compact: boolean
    /** The workspace element that scrolls the grid. */
    scroller?: HTMLElement | undefined
    /** The scroller's `scrollTop`; the grid writes it to reveal the cursor row. */
    scrollTop?: number
    /** The scroller's visible height. */
    viewport?: number
    onmove: (delta: -1 | 1) => void
    onhome: () => void
    /** A row click moves the cursor; the grid keeps focus. */
    oncursor: (index: number) => void
    /** A click on a row's selection box. */
    ontoggle: (identity: string, kind: RowKind) => void
    ontoggleall: () => void
    /** Drills into a grouped row. */
    onactivate: (identity: string, kind: RowKind) => void
    oninformation: (identity: string) => void
    onedit: (identity: string, field: 'merchant' | 'category') => void
    onsort: (field: SortField) => void
    /** Scrolling showed rows outside the loaded window, from the first fully visible row. */
    onshow?: (index: number) => void
  }

  let {
    projection,
    neighbors = [],
    cursorIndex,
    compact,
    scroller,
    scrollTop = $bindable(0),
    viewport = 520,
    onmove,
    onhome,
    oncursor,
    ontoggle,
    ontoggleall,
    onactivate,
    oninformation,
    onedit,
    onsort,
    onshow,
  }: Props = $props()
  let grid = $state<HTMLDivElement>()
  /** The row last passed to `onshow`. The cursor moving there follows the scroll, so stays. */
  let shown: number | undefined
  const height = $derived(rowHeight(compact))
  const columns = $derived(columnsFor(projection))
  const cellColumns = $derived(columns.filter((column) => column.key !== 'select'))
  const trackSizes = $derived(columns.map((column) => column.width).join(' '))
  const dimension = $derived(projection.view.sub_grouping ?? projection.view.grouping)
  const windows = $derived([projection, ...neighbors])
  const kind: RowKind = $derived(projection.detail_rows ? 'transaction' : 'aggregate')
  const dividers = $derived(projection.view.sort_field === 'amount' && kind === 'aggregate')
  // The scroller's top below the grid's top: the rows' offset under the sticky header. offsetTop
  // is read, not tracked, so content above the grid that resizes does not rerun this.
  const listTop = $derived(scrollTop - (grid?.offsetTop ?? 0))
  const slice = $derived(
    virtualSlice({
      scrollTop: listTop,
      viewport,
      count: projection.total_rows,
      overscan: 5,
      fixedHeight: height,
      heightOf: () => height,
    }),
  )
  const slots = $derived(
    Array.from({ length: slice.end - slice.start }, (_, offset) => {
      const index = slice.start + offset
      return { index, row: loadedRow(windows, index) }
    }),
  )
  const total = $derived(projection.total_rows)
  const range = $derived(visibleRange(listTop, viewport - height, height, total))
  const activeID = $derived(loadedRow(windows, cursorIndex)?.identity)
  // Effects that read this rerun only when the cursor moves, not on every prop update.
  const cursor = $derived(cursorIndex)

  // The grid keeps focus while the cursor moves, so the browser never scrolls the active row
  // into view. Reveal it here, or aria-activedescendant would name a row that is not rendered.
  $effect(() => {
    const index = cursor
    if (!grid) return
    untrack(() => {
      if (index !== shown) return reveal(index)
      // A window that arrived after more scrolling moves the cursor on to the row now on top.
      shown = undefined
      if (index !== range.first) oncursor(range.first)
    })
  })

  // A shrinking viewport (the selection bar opens) reveals the cursor row it covered. Only the
  // viewport is tracked, so scrolling the cursor row away does not pull it back.
  let lastViewport: number | undefined
  $effect(() => {
    const next = viewport
    untrack(() => {
      const before = visibleRange(listTop, (lastViewport ?? next) - height, height, total)
      lastViewport = next
      if (coveredCursor(before, range, cursor)) reveal(cursor)
    })
  })

  // One request per frame, for the latest first row, once scrolling leaves the cursor's window.
  $effect(() => {
    const index = rowToShow(range, cursor, projection.window)
    if (!onshow || index === undefined) return
    const frame = requestAnimationFrame(() => {
      shown = index
      onshow(index)
    })
    return () => cancelAnimationFrame(frame)
  })

  function reveal(index: number): void {
    const gridTop = grid?.offsetTop ?? 0
    const next = revealScrollTop(index, { scrollTop, viewport, rowHeight: height, gridTop })
    if (next === scrollTop) return
    scrollTop = next
    if (scroller) scroller.scrollTop = next
  }

  function keydown(event: KeyboardEvent): void {
    shown = undefined
    if (event.key === 'ArrowUp' || event.key === 'k') onmove(-1)
    else if (event.key === 'ArrowDown' || event.key === 'j') onmove(1)
    else if (event.key === 'Home') onhome()
    else return
    event.preventDefault()
    event.stopPropagation()
  }

  /** The loaded row a pointer event landed on, with the column key of its cell. */
  function hit(event: MouseEvent) {
    const target = pointerHit(event.target)
    const row = target && loadedRow(windows, target.index)
    return row && target ? { row, column: target.column } : undefined
  }

  function click(event: MouseEvent): void {
    const target = hit(event)
    if (!target) return
    if (target.column === 'select') ontoggle(target.row.identity, kind)
    else oncursor(target.row.index)
  }

  function doubleClick(event: MouseEvent): void {
    // A button's own click already acted; its double-click must not act again.
    if (event.target instanceof Element && event.target.closest('button')) return
    const target = hit(event)
    if (!target) return
    const identity = target.row.identity
    const action = doubleClickAction(kind, target.column)
    if (action === 'drill') onactivate(identity, kind)
    else if (action === 'information') oninformation(identity)
    else if (action) onedit(identity, action)
  }
</script>

{#if projection.total_rows === 0}
  <EmptyState
    title="No transactions"
    description="Change the current refinements to see results."
  />
{:else}
  <div
    class="mf-table"
    style:--mf-row-height={`${height}px`}
    style:--mf-columns={trackSizes}
    role="grid"
    aria-label="Financial results"
    aria-rowcount={projection.total_rows + 1}
    aria-activedescendant={activeID ? `moneyflow-row-${activeID}` : undefined}
    tabindex="0"
    bind:this={grid}
    onkeydown={keydown}
    onclick={click}
    ondblclick={doubleClick}
    onmousedown={(event) => {
      // The second press of a double-click would otherwise select a word of text.
      if (event.detail > 1) event.preventDefault()
    }}
  >
    <GridHeader
      {columns}
      view={projection.view}
      allSelected={projection.selection_count > 0 &&
        projection.selection_count === projection.total_rows}
      {onsort}
      {ontoggleall}
    />
    <div class="mf-table__spacer" style:height={`${slice.topPad}px`} aria-hidden="true"></div>
    {#each slots as { index, row } (index)}
      {#if row}
        {@const divider = dividers ? incomeDivider(windows, index) : null}
        <div
          id={`moneyflow-row-${row.identity}`}
          class={[
            'mf-table__row',
            {
              'mf-table__row--active': row.index === cursorIndex,
              'mf-table__row--selected': row.flags.selected,
              'mf-table__row--hidden': row.flags.hidden,
              'mf-table__row--divided': divider !== null,
              'mf-table__row--deleted': 'staged' in row && row.staged?.deleted === true,
            },
          ]}
          role="row"
          aria-rowindex={row.index + 2}
          aria-selected={row.flags.selected}
          data-index={row.index}
        >
          <div class="mf-table__cell mf-table__cell--select" role="gridcell" data-column="select">
            <!-- kit-ui-check-ignore: kit Checkbox lacks tabindex (upstream candidate) -->
            <span class={['mf-check', row.flags.selected && 'mf-check--on']} aria-hidden="true"
            ></span>
          </div>
          {#if 'merchant' in row}
            <DetailRow {row} columns={cellColumns} {oninformation} {onedit} />
          {:else if row.dimension === 'time'}
            <TimeRow {row} columns={cellColumns} {divider} />
          {:else}
            <AggregateRow {row} columns={cellColumns} {dimension} {divider} />
          {/if}
        </div>
      {:else}
        <div
          class="mf-table__row mf-table__row--placeholder"
          aria-hidden="true"
          data-index={index}
        ></div>
      {/if}
    {/each}
    <div
      class="mf-table__spacer"
      style:height={`${Math.max(0, slice.totalHeight - slice.topPad - slots.length * height)}px`}
      aria-hidden="true"
    ></div>
  </div>
{/if}
