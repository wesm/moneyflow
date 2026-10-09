<script lang="ts">
  import { IconButton, StatusDot } from '@kenn-io/kit-ui'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'

  import type { components } from '../../lib/api/schema'
  import { moneyCell } from '../../lib/table/cells'
  import { compactColumnIndex, type ColumnKey } from '../../lib/table/columns'
  import { outOfTabOrder } from '../../lib/table/pointer'

  interface Props {
    row: components['schemas']['AggregateRow']
    /** Names the group this row starts when Net changes sign; see `incomeDivider`. */
    divider?: 'Income' | 'Spending' | null
  }

  let { row, divider = null }: Props = $props()
  const layout = $derived(row.dimension === 'time' ? 'time' : 'aggregate')
  const nameKey = $derived(layout === 'time' ? 'period' : 'name')
  const column = (key: ColumnKey) => compactColumnIndex(layout, key)
</script>

<!-- Two lines: name and Net, then Count, In, and Out. A tap on the row drills. -->
<div class="mf-compact">
  <div
    class="mf-compact__lead"
    role="gridcell"
    data-column={nameKey}
    aria-colindex={column(nameKey)}
  >
    <span class="mf-table__name">
      <span class="mf-table__text">{row.label}</span>
      {#if row.flags.pending}<StatusDot status="stale" label="Pending edits" />{/if}
      {#if divider}<span class="mf-table__divider" aria-hidden="true">{divider}</span>{/if}
    </span>
  </div>
  <div class="mf-compact__figure" role="gridcell" data-column="net" aria-colindex={column('net')}>
    {moneyCell(row.total, 'unsigned')}
  </div>
  <div class="mf-compact__meta">
    <span role="gridcell" data-column="count" aria-colindex={column('count')}
      >{row.count} {row.count === 1 ? 'txn' : 'txns'}</span
    >
    <span class="mf-compact__money" role="gridcell" data-column="in" aria-colindex={column('in')}>
      <span class="mf-compact__label">In</span>
      {moneyCell(row.in, '—')}
    </span>
    <span class="mf-compact__money" role="gridcell" data-column="out" aria-colindex={column('out')}>
      <span class="mf-compact__label">Out</span>
      {moneyCell(row.out, '—')}
    </span>
  </div>
</div>
<div
  class="mf-compact__action"
  role="gridcell"
  data-column="actions"
  aria-colindex={column('actions')}
>
  <span class="mf-compact__button" {@attach outOfTabOrder}>
    <IconButton ariaLabel={`Open ${row.label}`} class="mf-touch-target">
      <ChevronRightIcon size={18} aria-hidden="true" />
    </IconButton>
  </span>
</div>
