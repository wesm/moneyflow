<script lang="ts">
  import { IconButton } from '@kenn-io/kit-ui'
  import PencilIcon from '@lucide/svelte/icons/pencil'

  import RowFlags from './RowFlags.svelte'
  import StagedValue from './StagedValue.svelte'
  import type { components } from '../../lib/api/schema'
  import { cellClass, type Column } from '../../lib/table/columns'
  import { outOfTabOrder } from '../../lib/table/pointer'

  type Field = 'merchant' | 'category'

  interface Props {
    row: components['schemas']['DetailRow']
    /** The row's columns after the selection column. */
    columns: Column[]
    oninformation: (identity: string) => void
    onedit: (identity: string, field: Field) => void
  }

  let { row, columns, oninformation, onedit }: Props = $props()
</script>

{#snippet editable(field: Field, committed: string, staged: string | undefined)}
  <span class="mf-table__editable">
    <span class="mf-table__text"><StagedValue {committed} {staged} /></span>
    <!-- Shown on the hovered and the cursor row; the m and c keys reach the same editors. -->
    <span class="mf-table__pencil" {@attach outOfTabOrder}>
      <IconButton size="sm" ariaLabel={`Edit ${field}`} onclick={() => onedit(row.identity, field)}>
        <PencilIcon size={13} aria-hidden="true" />
      </IconButton>
    </span>
  </span>
{/snippet}

{#each columns as column (column.key)}
  <div class={cellClass(column)} role="gridcell" data-column={column.key}>
    {#if column.key === 'date'}
      {row.date}
    {:else if column.key === 'merchant'}
      {@render editable('merchant', row.merchant, row.staged?.merchant)}
    {:else if column.key === 'category'}
      {@render editable('category', row.category, row.staged?.category)}
    {:else if column.key === 'account'}
      {row.account}
    {:else if column.key === 'amount'}
      {row.amount.display}
    {:else if column.key === 'flags'}
      <RowFlags {row} {oninformation} />
    {/if}
  </div>
{/each}
