<script lang="ts">
  import { StatusDot } from '@kenn-io/kit-ui'

  import type { components } from '../../lib/api/schema'
  import { contextCell, moneyCell, shareCell } from '../../lib/table/cells'
  import { cellClass, type Column } from '../../lib/table/columns'

  interface Props {
    row: components['schemas']['AggregateRow']
    /** The row's columns after the selection column. */
    columns: Column[]
    /** Names the group this row starts when Net changes sign; see `incomeDivider`. */
    divider?: 'Income' | 'Spending' | null
    /** The dimension the rows group by, which decides the Context cell. */
    dimension: string
  }

  let { row, columns, dimension, divider = null }: Props = $props()
  const share = $derived(shareCell(row))
  const spending = $derived(row.total.minor.startsWith('-'))
</script>

{#each columns as column (column.key)}
  <div class={cellClass(column)} role="gridcell" data-column={column.key}>
    {#if column.key === 'name'}
      <span class="mf-table__name">
        <span class="mf-table__text">{row.label}</span>
        {#if row.flags.pending}<StatusDot status="stale" label="Pending edits" />{/if}
        {#if divider}<span class="mf-table__divider" aria-hidden="true">{divider}</span>{/if}
      </span>
    {:else if column.key === 'context'}
      <span class="mf-table__muted">{contextCell(row, dimension)}</span>
    {:else if column.key === 'count'}
      {row.count}
    {:else if column.key === 'in'}
      {moneyCell(row.in, '—')}
    {:else if column.key === 'out'}
      {moneyCell(row.out, '—')}
    {:else if column.key === 'net'}
      {moneyCell(row.total, 'unsigned')}
    {:else if column.key === 'share'}
      <span class="mf-share">
        {share.label}
        <span class="mf-share__track" aria-hidden="true">
          <span
            class={['mf-share__bar', spending ? 'mf-share__bar--out' : 'mf-share__bar--in']}
            style:width={`${share.ratio * 100}%`}
          ></span>
        </span>
      </span>
    {/if}
  </div>
{/each}
