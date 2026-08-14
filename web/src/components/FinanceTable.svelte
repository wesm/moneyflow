<script lang="ts">
  import { EmptyState, virtualSlice } from '@kenn-io/kit-ui'
  import type { ViewProjection } from '../lib/api/client'

  interface Props {
    projection: ViewProjection
    cursorIndex: number
    onmove: (delta: -1 | 1) => void
    onhome: () => void
    onactivate: (identity: string, kind: 'detail' | 'aggregate') => void
    onselect: (identity: string, kind: 'detail' | 'aggregate') => void
  }
  let { projection, cursorIndex, onmove, onhome, onactivate, onselect }: Props = $props()
  let scrollTop = $state(0)
  let viewport = $state(520)
  const rows = $derived(projection.detail_rows ?? projection.aggregate_rows ?? [])
  const detail = $derived(Array.isArray(projection.detail_rows))
  const slice = $derived(
    virtualSlice({
      scrollTop,
      viewport,
      count: rows.length,
      overscan: 5,
      fixedHeight: 34,
      heightOf: () => 34,
    }),
  )
  const visible = $derived(rows.slice(slice.start, slice.end))
  const activeID = $derived(rows.find((row) => row.index === cursorIndex)?.identity)
  function sortState(field: string): 'ascending' | 'descending' | undefined {
    return projection.view.sort_field === field
      ? projection.view.sort_direction === 'asc'
        ? 'ascending'
        : 'descending'
      : undefined
  }
  function groupingSortField(): string {
    return projection.view.grouping === 'time' ? 'time_period' : projection.view.grouping
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'ArrowUp' || event.key === 'k') onmove(-1)
    else if (event.key === 'ArrowDown' || event.key === 'j') onmove(1)
    else if (event.key === 'Home') onhome()
    else return
    event.preventDefault()
    event.stopPropagation()
  }
</script>

{#if rows.length === 0}
  <EmptyState
    title="No transactions"
    description="Change the current refinements to see results."
  />
{:else}
  <div
    class="finance-grid"
    role="grid"
    aria-label="Financial results"
    aria-rowcount={projection.total_rows + 1}
    aria-activedescendant={activeID ? `moneyflow-row-${activeID}` : undefined}
    tabindex="0"
    onkeydown={keydown}
    onscroll={(event) => (scrollTop = event.currentTarget.scrollTop)}
    bind:clientHeight={viewport}
  >
    <div class="finance-grid__header" role="row">
      {#if detail}
        <!-- kit-ui-check-ignore: virtual ARIA grid cannot contain table header cells -->
        <div role="columnheader" aria-sort={sortState('date')}>Date</div>
        <!-- kit-ui-check-ignore: virtual ARIA grid cannot contain table header cells -->
        <div role="columnheader" aria-sort={sortState('account')}>Account</div>
        <!-- kit-ui-check-ignore: virtual ARIA grid cannot contain table header cells -->
        <div role="columnheader" aria-sort={sortState('merchant')}>Merchant</div>
        <!-- kit-ui-check-ignore: virtual ARIA grid cannot contain table header cells -->
        <div role="columnheader" aria-sort={sortState('category')}>Category</div>
      {:else}
        <!-- kit-ui-check-ignore: virtual ARIA grid cannot contain table header cells -->
        <div role="columnheader" aria-sort={sortState(groupingSortField())}>
          {projection.view.grouping}
        </div>
        <!-- kit-ui-check-ignore: virtual ARIA grid cannot contain table header cells -->
        <div role="columnheader" aria-sort={sortState('count')}>Count</div>
      {/if}
      <!-- kit-ui-check-ignore: virtual ARIA grid cannot contain table header cells -->
      <div role="columnheader" aria-sort={sortState('amount')}>Amount</div>
    </div>
    <div style={`height:${slice.topPad}px`} aria-hidden="true"></div>
    {#each visible as row (row.identity)}
      <div
        id={`moneyflow-row-${row.identity}`}
        class:finance-grid__row--active={row.index === cursorIndex}
        class:finance-grid__row--hidden={row.flags.hidden}
        role="row"
        tabindex="-1"
        aria-rowindex={row.index + 2}
        aria-selected={row.flags.selected}
        ondblclick={() => {
          if (!detail) onactivate(row.identity, 'aggregate')
        }}
        onclick={() => onselect(row.identity, detail ? 'detail' : 'aggregate')}
        onkeydown={(event) => {
          if (event.key === 'Enter' && !detail) onactivate(row.identity, 'aggregate')
          if (event.key === ' ') onselect(row.identity, detail ? 'detail' : 'aggregate')
        }}
      >
        {#if 'merchant' in row}
          <div role="gridcell">{row.date}</div>
          <div role="gridcell">{row.account}</div>
          <div role="gridcell">{row.merchant}</div>
          <div role="gridcell">{row.category}</div>
          <div class="money" role="gridcell">{row.amount.display}</div>
        {:else}
          <div role="gridcell">{row.label}</div>
          <div role="gridcell">{row.count}</div>
          <div class="money" role="gridcell">{row.total.display}</div>
        {/if}
      </div>
    {/each}
    <div
      style={`height:${Math.max(0, slice.totalHeight - slice.topPad - visible.length * 34)}px`}
      aria-hidden="true"
    ></div>
  </div>
{/if}
