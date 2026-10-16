<script lang="ts">
  import { Tooltip } from '@kenn-io/kit-ui'

  import CashflowTooltip from './CashflowTooltip.svelte'
  import type { ColumnGeometry, CursorBar, RowAmounts } from '../../lib/charts/cashflow'

  interface Props {
    column: ColumnGeometry
    zeroY: number
    /** The bars' drawing height; the axis label sits below it. */
    height: number
    /** The cursor's share of this period, drawn solid over the column. */
    cursor?: CursorBar | undefined
    row?: RowAmounts | undefined
    /** The axis label; empty where a crowded chart labels only every few columns. */
    tick: string
    /** The chart's one Tab stop. */
    active: boolean
    /** Inside a drag's range. */
    dragged: boolean
    onclick: (event: MouseEvent) => void
    onkeydown: (event: KeyboardEvent) => void
    onpointerdown: (event: PointerEvent) => void
    onpointerenter: () => void
    onfocus: () => void
  }

  let {
    column,
    zeroY,
    height,
    cursor,
    row,
    tick,
    active,
    dragged,
    onclick,
    onkeydown,
    onpointerdown,
    onpointerenter,
    onfocus,
  }: Props = $props()
  const id = $props.id()
  // Kit describes its wrapper, not the button, so the focused column carries the row's amounts.
  const described = $derived(active && row !== undefined)
</script>

<Tooltip>
  {#snippet content()}
    <CashflowTooltip {column} {row} />
  {/snippet}
  <button
    type="button"
    class={[
      'mf-cashflow__column',
      column.inScope && 'mf-cashflow__column--scope',
      column.partial && 'mf-cashflow__column--partial',
      dragged && 'mf-cashflow__column--dragged',
    ]}
    aria-label={column.name}
    aria-describedby={described ? id : undefined}
    tabindex={active ? 0 : -1}
    {onclick}
    {onkeydown}
    {onpointerdown}
    {onpointerenter}
    {onfocus}
  >
    <svg width="100%" {height} aria-hidden="true">
      <rect class="mf-cashflow__in" x="20%" width="60%" y={column.inTop} height={column.inHeight} />
      <rect class="mf-cashflow__out" x="20%" width="60%" y={zeroY} height={column.outHeight} />
      {#if cursor}
        <rect
          class="mf-cashflow__in mf-cashflow__cursor"
          x="20%"
          width="60%"
          y={cursor.inTop}
          height={cursor.inHeight}
        />
        <rect
          class="mf-cashflow__out mf-cashflow__cursor"
          x="20%"
          width="60%"
          y={zeroY}
          height={cursor.outHeight}
        />
      {/if}
    </svg>
    <span class="mf-cashflow__tick" aria-hidden="true">{tick}</span>
  </button>
  {#if described && row}
    <span {id} class="kit-sr-only">{row.label}, in {row.in}, out {row.out}</span>
  {/if}
</Tooltip>
