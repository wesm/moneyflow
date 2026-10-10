<script lang="ts">
  import { EmptyState } from '@kenn-io/kit-ui'
  import { MediaQuery } from 'svelte/reactivity'

  import GridHeader from './GridHeader.svelte'
  import GridRow from './GridRow.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { components } from '../../lib/api/schema'
  import { incomeDivider } from '../../lib/table/cells'
  import { ColumnRoom } from '../../lib/table/column-room.svelte'
  import {
    columnsAtWidth,
    columnsFor,
    columnsThatFit,
    compactColumnCount,
    compactLayout,
    WIDE_COLUMNS_QUERY,
    type SortField,
  } from '../../lib/table/columns'
  import { GridScroll } from '../../lib/table/grid-scroll.svelte'
  import { rowHeight } from '../../lib/table/layout'
  import {
    compactTapAction,
    doubleClickAction,
    pointerHit,
    type CompactTapAction,
    type RowKind,
  } from '../../lib/table/pointer'
  import type { RowInsights } from '../../lib/table/trend'
  import { loadedRow, type TableRow } from '../../lib/table/virtual-rows'

  interface Props {
    /** The loaded window of the result. */
    projection: ViewProjection
    /** Cached windows next to it; their rows draw instead of placeholders. */
    neighbors?: readonly ViewProjection[]
    /** The displayed view's insights, for grouped rows' Trend and vs prior. */
    insights?: RowInsights | undefined
    cursorIndex: number
    /** Two-line phone rows at 640 px and narrower. */
    compact: boolean
    /** The workspace element that scrolls the grid. */
    scroller?: HTMLElement | undefined
    /** The scroller's `scrollTop`; the grid writes it to reveal the cursor row. */
    scrollTop?: number
    /** The scroller's visible height. */
    viewport?: number
    /** The measured height of the content above the grid in the scroller. */
    above?: number
    onmove: (delta: -1 | 1) => void
    onhome: () => void
    /** A row click moves the cursor; the grid keeps focus. Settles once the cursor moved. */
    oncursor: (index: number) => Promise<void>
    /** A click on a row's selection box. */
    ontoggle: (identity: string, kind: RowKind) => void
    ontoggleall: () => void
    /** Drills into a grouped row. */
    onactivate: (identity: string, kind: RowKind) => void
    oninformation: (identity: string) => void
    onedit: (identity: string, field: 'merchant' | 'category') => void
    onsort: (field: SortField) => void
    /** A compact transaction row's ⋯ button, which focus returns to after the sheet. */
    onactions: (row: components['schemas']['DetailRow'], opener: HTMLElement) => void
    /** Asks for the window holding the first fully visible row, or the cursor row to withdraw. */
    onshow?: (index: number) => void
  }

  let {
    projection,
    neighbors = [],
    insights,
    cursorIndex,
    compact,
    scroller,
    scrollTop = $bindable(0),
    viewport = 520,
    above = 0,
    onmove,
    onhome,
    oncursor,
    ontoggle,
    ontoggleall,
    onactivate,
    oninformation,
    onedit,
    onsort,
    onactions,
    onshow,
  }: Props = $props()
  let grid = $state<HTMLDivElement>()
  const height = $derived(rowHeight(compact))
  const wide = new MediaQuery(WIDE_COLUMNS_QUERY)
  const comparison = $derived(insights?.response?.ranges.comparison_label)
  const room = new ColumnRoom(() => scroller)
  const all = $derived(columnsAtWidth(columnsFor(projection, comparison), wide.current))
  const columns = $derived(columnsThatFit(all, room.rem))
  const layout = $derived(compact ? compactLayout(projection) : undefined)
  const cellColumns = $derived(columns.filter((column) => column.key !== 'select'))
  const trackSizes = $derived(columns.map((column) => column.width).join(' '))
  const dimension = $derived(projection.view.sub_grouping ?? projection.view.grouping)
  const windows = $derived([projection, ...neighbors])
  const kind: RowKind = $derived(projection.detail_rows ? 'transaction' : 'aggregate')
  const dividers = $derived(projection.view.sort_field === 'amount' && kind === 'aggregate')
  const activeID = $derived(loadedRow(windows, cursorIndex)?.identity)
  const scroll = new GridScroll({
    get grid() {
      return grid
    },
    get scroller() {
      return scroller
    },
    get windows() {
      return windows
    },
    get cursorIndex() {
      return cursorIndex
    },
    get rowHeight() {
      return height
    },
    get viewport() {
      return viewport
    },
    get above() {
      return above
    },
    get scrollTop() {
      return scrollTop
    },
    set scrollTop(next) {
      scrollTop = next
    },
    oncursor: (index) => void oncursor(index),
    get onshow() {
      return onshow
    },
  })

  function keydown(event: KeyboardEvent): void {
    if (!['ArrowUp', 'k', 'ArrowDown', 'j', 'Home'].includes(event.key)) return
    // A row button keeps focus after its sheet closes; the grid takes it back, as its keys expect.
    if (event.target !== grid) grid?.focus({ preventScroll: true })
    if (event.key === 'ArrowUp' || event.key === 'k') onmove(-1)
    else if (event.key === 'ArrowDown' || event.key === 'j') onmove(1)
    else onhome()
    scroll.keyed()
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
    if (compact) tap(target.row, compactTapAction(kind, target.column), event)
    else if (target.column === 'select') ontoggle(target.row.identity, kind)
    else void oncursor(target.row.index)
  }

  /** A tap on a compact row: toggle, drill, or move the cursor there and open Info or actions. */
  function tap(row: TableRow, action: CompactTapAction, event: MouseEvent) {
    if (action === 'toggle') return ontoggle(row.identity, kind)
    // On the tapped row first, so Back returns there.
    if (action === 'drill')
      return void oncursor(row.index).then(() => onactivate(row.identity, kind))
    void oncursor(row.index)
    const opener = event.target instanceof Element && event.target.closest('button')
    if (action === 'information') oninformation(row.identity)
    else if ('merchant' in row && opener) onactions(row, opener)
  }

  function doubleClick(event: MouseEvent): void {
    // A button's own click already acted; its double-click must not act again. Taps act once.
    if (compact || (event.target instanceof Element && event.target.closest('button'))) return
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
    class={['mf-table', compact && 'mf-table--compact']}
    style:--mf-row-height={`${height}px`}
    style:--mf-columns={compact ? undefined : trackSizes}
    role="grid"
    aria-label="Financial results"
    aria-rowcount={projection.total_rows + 1}
    aria-colcount={layout && compactColumnCount(layout)}
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
      compact={layout}
      view={projection.view}
      allSelected={projection.selection_count > 0 &&
        projection.selection_count === projection.total_rows}
      {onsort}
      {ontoggleall}
    />
    <div class="mf-table__spacer" style:height={`${scroll.pads.top}px`} aria-hidden="true"></div>
    {#each scroll.slots as { index, row } (index)}
      {#if row}
        <GridRow
          {row}
          active={row.index === cursorIndex}
          divider={dividers ? incomeDivider(windows, index) : null}
          {layout}
          columns={cellColumns}
          {dimension}
          {insights}
          {oninformation}
          {onedit}
        />
      {:else}
        <div
          class="mf-table__row mf-table__row--placeholder"
          aria-hidden="true"
          data-index={index}
        ></div>
      {/if}
    {/each}
    <div class="mf-table__spacer" style:height={`${scroll.pads.bottom}px`} aria-hidden="true"></div>
  </div>
{/if}
