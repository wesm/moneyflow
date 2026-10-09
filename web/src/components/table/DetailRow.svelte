<script lang="ts">
  import RowFlags from './RowFlags.svelte'
  import StagedValue from './StagedValue.svelte'
  import type { components } from '../../lib/api/schema'
  import { cellClass, type Column } from '../../lib/table/columns'

  interface Props {
    row: components['schemas']['DetailRow']
    /** The row's columns after the selection column. */
    columns: Column[]
    oninformation?: ((identity: string) => void) | undefined
  }

  let { row, columns, oninformation }: Props = $props()
</script>

{#each columns as column (column.key)}
  <div class={cellClass(column)} role="gridcell" data-column={column.key}>
    {#if column.key === 'date'}
      {row.date}
    {:else if column.key === 'merchant'}
      <StagedValue committed={row.merchant} staged={row.staged?.merchant} />
    {:else if column.key === 'category'}
      <StagedValue committed={row.category} staged={row.staged?.category} />
    {:else if column.key === 'account'}
      {row.account}
    {:else if column.key === 'amount'}
      {row.amount.display}
    {:else if column.key === 'flags'}
      <RowFlags {row} {oninformation} />
    {/if}
  </div>
{/each}
