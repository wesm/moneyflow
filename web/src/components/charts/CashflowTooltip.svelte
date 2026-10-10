<script lang="ts">
  import { dashZero, type ColumnGeometry, type RowAmounts } from '../../lib/charts/cashflow'

  interface Props {
    column: ColumnGeometry
    /** The cursor row's share of this period, when the cursor is a row. */
    row?: RowAmounts | undefined
  }

  let { column, row }: Props = $props()
</script>

<p class="mf-cashflow-tip__title">{column.label}</p>
<dl class="mf-cashflow-tip">
  <dt>In</dt>
  <dd>{dashZero(column.in)}</dd>
  <dt>Out</dt>
  <dd>{dashZero(column.out)}</dd>
  <dt>Net</dt>
  <dd>{column.net}</dd>
</dl>
{#if row}
  <p class="mf-cashflow-tip__title">{row.label}</p>
  <dl class="mf-cashflow-tip">
    <dt>In</dt>
    <dd>{dashZero(row.in)}</dd>
    <dt>Out</dt>
    <dd>{dashZero(row.out)}</dd>
  </dl>
{/if}
