<script lang="ts">
  import AggregateRow from './AggregateRow.svelte'
  import CompactAggregateRow from './CompactAggregateRow.svelte'
  import CompactDetailRow from './CompactDetailRow.svelte'
  import DetailRow from './DetailRow.svelte'
  import TimeRow from './TimeRow.svelte'
  import type { Column, CompactLayout } from '../../lib/table/columns'
  import type { TableRow } from '../../lib/table/virtual-rows'

  interface Props {
    row: TableRow
    /** The cursor is on this row. */
    active: boolean
    /** Names the group this row starts when Net changes sign; see `incomeDivider`. */
    divider: 'Income' | 'Spending' | null
    /** The phone row layout at 640 px and narrower; absent for desktop rows. */
    layout: CompactLayout | undefined
    /** The desktop columns after the selection column. */
    columns: Column[]
    /** The dimension the rows group by, which decides a grouped row's Context cell. */
    dimension: string
    oninformation: (identity: string) => void
    onedit: (identity: string, field: 'merchant' | 'category') => void
  }

  let { row, active, divider, layout, columns, dimension, oninformation, onedit }: Props = $props()
</script>

<div
  id={`moneyflow-row-${row.identity}`}
  class={[
    'mf-table__row',
    {
      'mf-table__row--active': active,
      'mf-table__row--selected': row.flags.selected,
      'mf-table__row--hidden': row.flags.hidden,
      'mf-table__row--divided': divider !== null,
      'mf-table__row--deleted': 'staged' in row && row.staged?.deleted === true,
    },
  ]}
  role="row"
  aria-rowindex={row.index + 2}
  aria-selected={row.flags.selected}
  data-index={row.index}
>
  <div class="mf-table__cell" role="gridcell" data-column="select" aria-colindex={layout && 1}>
    <!-- kit-ui-check-ignore: kit Checkbox lacks tabindex (upstream candidate) -->
    <span class={['mf-check', row.flags.selected && 'mf-check--on']} aria-hidden="true"></span>
  </div>
  {#if layout}
    {#if 'merchant' in row}
      <CompactDetailRow {row} {oninformation} />
    {:else}
      <CompactAggregateRow {row} {divider} />
    {/if}
  {:else if 'merchant' in row}
    <DetailRow {row} {columns} {oninformation} {onedit} />
  {:else if row.dimension === 'time'}
    <TimeRow {row} {columns} {divider} />
  {:else}
    <AggregateRow {row} {columns} {dimension} {divider} />
  {/if}
</div>
