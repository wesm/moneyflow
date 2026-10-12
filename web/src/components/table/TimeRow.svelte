<script lang="ts">
  import RowName from './RowName.svelte'
  import type { components } from '../../lib/api/schema'
  import { moneyCell, netBar, savingsCell } from '../../lib/table/cells'
  import { cellClass, type Column } from '../../lib/table/columns'

  interface Props {
    row: components['schemas']['AggregateRow']
    /** The row's columns after the selection column. */
    columns: Column[]
    /** Names the group this row starts when Net changes sign; see `incomeDivider`. */
    divider?: 'Income' | 'Spending' | null
  }

  let { row, columns, divider = null }: Props = $props()
  const bar = $derived(netBar(row.net_ratio))
</script>

{#each columns as column (column.key)}
  <div class={cellClass(column)} role="gridcell" data-column={column.key}>
    {#if column.key === 'period'}
      <RowName label={row.label} pending={row.flags.pending} {divider} />
    {:else if column.key === 'count'}
      {row.count}
    {:else if column.key === 'in'}
      {moneyCell(row.in, '—')}
    {:else if column.key === 'out'}
      {moneyCell(row.out, '—')}
    {:else if column.key === 'net'}
      {moneyCell(row.total, 'unsigned')}
    {:else if column.key === 'net_bar'}
      <span class="mf-net-bar" aria-hidden="true">
        {#if bar}
          <span
            class={['mf-net-bar__fill', `mf-net-bar__fill--${bar.side}`]}
            style:width={`${bar.percent}%`}
          ></span>
        {/if}
      </span>
    {:else if column.key === 'savings'}
      {savingsCell(row)}
    {/if}
  </div>
{/each}
