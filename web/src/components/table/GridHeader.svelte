<script lang="ts">
  import { IconButton } from '@kenn-io/kit-ui'

  import type { ViewProjection } from '../../lib/api/client'
  import { cellClass, type Column, type ColumnKey, type SortField } from '../../lib/table/columns'
  import { outOfTabOrder } from '../../lib/table/pointer'

  interface Props {
    columns: Column[]
    view: ViewProjection['view']
    /** Every matching row is selected, so the header box shows checked. */
    allSelected: boolean
    onsort: (field: SortField) => void
    ontoggleall: () => void
  }

  let { columns, view, allSelected, onsort, ontoggleall }: Props = $props()
  /** Columns whose content explains itself; their headers are named for assistive technology. */
  const unlabelled = new Set<ColumnKey>(['net_bar', 'flags'])

  function sortState(column: Column): 'ascending' | 'descending' | undefined {
    if (!column.sortField || column.sortField !== view.sort_field) return undefined
    return view.sort_direction === 'asc' ? 'ascending' : 'descending'
  }
</script>

<div class="mf-table__header" role="row">
  {#each columns as column (column.key)}
    {@const sort = sortState(column)}
    {@const field = column.sortField}
    <!-- kit-ui-check-ignore: virtual ARIA grid cannot contain table header cells -->
    <div class={cellClass(column)} role="columnheader" data-column={column.key} aria-sort={sort}>
      {#if column.key === 'select'}
        <span class="mf-table__select-all" {@attach outOfTabOrder}>
          <IconButton size="sm" ariaLabel={column.label} onclick={ontoggleall}>
            <!-- kit-ui-check-ignore: kit Checkbox lacks tabindex (upstream candidate) -->
            <span class={['mf-check', allSelected && 'mf-check--on']} aria-hidden="true"></span>
          </IconButton>
        </span>
      {:else if field}
        <button class="mf-table__sort" type="button" tabindex="-1" onclick={() => onsort(field)}
          >{column.label}</button
        >
      {:else if unlabelled.has(column.key)}
        <span class="kit-sr-only">{column.label}</span>
      {:else}
        {column.label}
      {/if}
    </div>
  {/each}
</div>
