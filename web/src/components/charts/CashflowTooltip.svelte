<script lang="ts">
  import type { ColumnGeometry, RowAmounts } from '../../lib/charts/cashflow'

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
  <dd>{column.in}</dd>
  <dt>Out</dt>
  <dd>{column.out}</dd>
  <dt>Net</dt>
  <dd>{column.net}</dd>
</dl>
{#if row}
  <p class="mf-cashflow-tip__title">{row.label}</p>
  <dl class="mf-cashflow-tip">
    <dt>In</dt>
    <dd>{row.in}</dd>
    <dt>Out</dt>
    <dd>{row.out}</dd>
  </dl>
{/if}
