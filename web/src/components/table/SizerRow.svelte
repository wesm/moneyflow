<script lang="ts">
  import { IconButton } from '@kenn-io/kit-ui'
  import PencilIcon from '@lucide/svelte/icons/pencil'

  import RowName from './RowName.svelte'
  import StagedValue from './StagedValue.svelte'
  import { contextCell } from '../../lib/table/cells'
  import { cellClass, type Column } from '../../lib/table/columns'
  import type { TableRow } from '../../lib/table/virtual-rows'

  interface Props {
    row: TableRow
    /** Every column shown, the selection column first. */
    columns: Column[]
    /** The dimension the rows group by, which decides the Context cell. */
    dimension: string
    /** A grouped row may carry an Income or Spending divider; keep room for the longer one. */
    divider: 'Spending' | null
  }

  let { row, columns, dimension, divider }: Props = $props()
</script>

<!--
  An invisible, zero-height row that draws a row's labels as the rows on screen do, so the grid's
  text columns fit them; see `ColumnSizer`. Only its text cells have content.
-->
<div class="mf-table__sizer" aria-hidden="true" inert>
  {#each columns as column (column.key)}
    <div class={cellClass(column)}>
      {#if 'merchant' in row}
        {#if column.key === 'merchant' || column.key === 'category'}
          <!-- The same parts as DetailRow's editable cell, with the room its pencil keeps. -->
          <span class="mf-table__editable">
            <span class="mf-table__text"
              ><StagedValue committed={row[column.key]} staged={row.staged?.[column.key]} /></span
            >
            <span class="mf-table__pencil">
              <IconButton size="sm" ariaLabel="Edit">
                <PencilIcon size={13} aria-hidden="true" />
              </IconButton>
            </span>
          </span>
        {:else if column.key === 'account'}
          {row.account}
        {/if}
      {:else if column.key === 'context'}
        <span class="mf-table__muted">{contextCell(row, dimension)}</span>
      {:else if column.key === 'name' || column.key === 'period'}
        <RowName label={row.label} pending={row.flags.pending} {divider} />
      {/if}
    </div>
  {/each}
</div>
