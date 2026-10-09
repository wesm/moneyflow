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
  import { rowHeight } from '../../lib/table/layout'

  type RowKind = 'transaction' | 'aggregate'

  interface Props {
    projection: ViewProjection
    cursorIndex: number
    /** Two-line phone rows at 640 px and narrower. */
    compact: boolean
    onmove: (delta: -1 | 1) => void
    onhome: () => void
    oncursor?: (index: number) => void
    ontoggle: (identity: string, kind: RowKind) => void
    ontoggleall?: () => void
    onactivate: (identity: string, kind: RowKind) => void
    oninformation?: (identity: string) => void
    onedit?: (identity: string, field: 'merchant' | 'category') => void
    onsort?: (field: SortField) => void
    onshow?: (index: number) => void
  }

  let {
    projection,
    cursorIndex,
    compact,
    onmove,
    onhome,
    ontoggle,
    onactivate,
    oninformation,
  }: Props = $props()
  let grid = $state<HTMLDivElement>()
  let scrollTop = $state(0)
  let viewport = $state(520)
  const height = $derived(rowHeight(compact))
  const columns = $derived(columnsFor(projection))
  const cellColumns = $derived(columns.filter((column) => column.key !== 'select'))
  const trackSizes = $derived(columns.map((column) => column.width).join(' '))
  const dimension = $derived(projection.view.sub_grouping ?? projection.view.grouping)
  const aggregates = $derived(projection.aggregate_rows ?? [])
  const rows = $derived(projection.detail_rows ?? aggregates)
  const kind: RowKind = $derived(projection.detail_rows ? 'transaction' : 'aggregate')
  const dividers = $derived(projection.view.sort_field === 'amount' && kind === 'aggregate')
  const slice = $derived(
    virtualSlice({
      scrollTop,
      viewport,
      count: rows.length,
      overscan: 5,
      fixedHeight: height,
      heightOf: () => height,
    }),
  )
  const visible = $derived(rows.slice(slice.start, slice.end))
  const activePosition = $derived(rows.findIndex((row) => row.index === cursorIndex))
  const activeID = $derived(rows[activePosition]?.identity)

  // The grid keeps focus while the cursor moves, so the browser never scrolls the active row
  // into view. Reveal it here, or aria-activedescendant would name a row that is not rendered.
  $effect(() => {
    const top = activePosition * height
    if (activePosition < 0 || !grid) return
    const current = untrack(() => scrollTop)
    const rowsViewport = untrack(() => viewport) - height
    let next = current
    if (top < current) next = top
    else if (top + height > current + rowsViewport) next = top + height - rowsViewport
    if (next === current) return
    scrollTop = next
    grid.scrollTop = next
  })

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'ArrowUp' || event.key === 'k') onmove(-1)
    else if (event.key === 'ArrowDown' || event.key === 'j') onmove(1)
    else if (event.key === 'Home') onhome()
    else return
    event.preventDefault()
    event.stopPropagation()
  }

  function activate(identity: string): void {
    if (kind === 'transaction') oninformation?.(identity)
    else onactivate(identity, 'aggregate')
  }
</script>

{#if rows.length === 0}
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
    onscroll={(event) => (scrollTop = event.currentTarget.scrollTop)}
    bind:clientHeight={viewport}
  >
    <GridHeader {columns} view={projection.view} />
    <div class="mf-table__spacer" style:height={`${slice.topPad}px`} aria-hidden="true"></div>
    {#each visible as row, offset (row.identity)}
      {@const divider = dividers ? incomeDivider(aggregates, slice.start + offset) : null}
      <div
        id={`moneyflow-row-${row.identity}`}
        class={[
          'mf-table__row',
          {
            'mf-table__row--active': row.index === cursorIndex,
            'mf-table__row--selected': row.flags.selected,
            'mf-table__row--hidden': row.flags.hidden,
          },
        ]}
        role="row"
        tabindex="-1"
        aria-rowindex={row.index + 2}
        aria-selected={row.flags.selected}
        ondblclick={() => activate(row.identity)}
        onclick={() => ontoggle(row.identity, kind)}
        onkeydown={(event) => {
          if (event.key === 'Enter') activate(row.identity)
          if (event.key === ' ') ontoggle(row.identity, kind)
        }}
      >
        {#if divider}
          <span class="mf-table__divider" aria-hidden="true"><span>{divider}</span></span>
        {/if}
        <div class="mf-table__cell mf-table__cell--select" role="gridcell" data-column="select">
          <!-- kit-ui-check-ignore: kit Checkbox lacks tabindex (upstream candidate) -->
          <span class={['mf-check', row.flags.selected && 'mf-check--on']} aria-hidden="true"
          ></span>
        </div>
        {#if 'merchant' in row}
          <DetailRow {row} columns={cellColumns} {oninformation} />
        {:else if row.dimension === 'time'}
          <TimeRow {row} columns={cellColumns} />
        {:else}
          <AggregateRow {row} columns={cellColumns} {dimension} />
        {/if}
      </div>
    {/each}
    <div
      class="mf-table__spacer"
      style:height={`${Math.max(0, slice.totalHeight - slice.topPad - visible.length * height)}px`}
      aria-hidden="true"
    ></div>
  </div>
{/if}
