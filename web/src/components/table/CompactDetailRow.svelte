<script lang="ts">
  import { IconButton } from '@kenn-io/kit-ui'
  import EllipsisIcon from '@lucide/svelte/icons/ellipsis'

  import RowFlags from './RowFlags.svelte'
  import StagedValue from './StagedValue.svelte'
  import type { components } from '../../lib/api/schema'
  import { compactColumnIndex, type ColumnKey } from '../../lib/table/columns'
  import { outOfTabOrder } from '../../lib/table/pointer'

  interface Props {
    row: components['schemas']['DetailRow']
    oninformation: (identity: string) => void
  }

  let { row, oninformation }: Props = $props()
  const column = (key: ColumnKey) => compactColumnIndex('detail', key)
</script>

<!-- Two lines: merchant and amount, then date, category, and account. A tap opens Info. -->
<div class="mf-compact">
  <div
    class="mf-compact__lead"
    role="gridcell"
    data-column="merchant"
    aria-colindex={column('merchant')}
  >
    <span class="mf-table__text">
      <StagedValue committed={row.merchant} staged={row.staged?.merchant} />
    </span>
  </div>
  <div
    class="mf-compact__figure"
    role="gridcell"
    data-column="amount"
    aria-colindex={column('amount')}
  >
    {row.amount.display}
  </div>
  <div class="mf-compact__meta">
    <span class="mf-compact__date" role="gridcell" data-column="date" aria-colindex={column('date')}
      >{row.date}</span
    >
    <span
      class="mf-compact__text"
      role="gridcell"
      data-column="category"
      aria-colindex={column('category')}
    >
      <StagedValue committed={row.category} staged={row.staged?.category} />
    </span>
    <span
      class="mf-compact__text"
      role="gridcell"
      data-column="account"
      aria-colindex={column('account')}>{row.account}</span
    >
    <span
      class="mf-compact__flags"
      role="gridcell"
      data-column="flags"
      aria-colindex={column('flags')}
    >
      <RowFlags {row} {oninformation} />
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
    <IconButton
      ariaLabel={`Actions for ${row.merchant}`}
      ariaHaspopup="dialog"
      class="mf-touch-target"
    >
      <EllipsisIcon size={18} aria-hidden="true" />
    </IconButton>
  </span>
</div>
