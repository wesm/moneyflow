<script lang="ts">
  import type { ViewProjection } from '../../lib/api/client'
  import { cellClass, type Column, type ColumnKey } from '../../lib/table/columns'

  interface Props {
    columns: Column[]
    view: ViewProjection['view']
  }

  let { columns, view }: Props = $props()
  /** Columns whose content explains itself; their headers are named for assistive technology. */
  const unlabelled = new Set<ColumnKey>(['select', 'net_bar', 'flags'])

  function sortState(column: Column): 'ascending' | 'descending' | undefined {
    if (!column.sortField || column.sortField !== view.sort_field) return undefined
    return view.sort_direction === 'asc' ? 'ascending' : 'descending'
  }
</script>

<div class="mf-table__header" role="row">
  {#each columns as column (column.key)}
    <!-- kit-ui-check-ignore: virtual ARIA grid cannot contain table header cells -->
    <div class={cellClass(column)} role="columnheader" aria-sort={sortState(column)}>
      {#if unlabelled.has(column.key)}
        <span class="kit-sr-only">{column.label}</span>
      {:else}
        {column.label}
      {/if}
    </div>
  {/each}
</div>
