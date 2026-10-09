<script lang="ts">
  import { StatusDot } from '@kenn-io/kit-ui'

  interface Props {
    /** The group's name, such as a merchant or a period. */
    label: string
    /** Some of the group's transactions have pending edits. */
    pending: boolean
    /** Names the group this row starts when Net changes sign; see `incomeDivider`. */
    divider: 'Income' | 'Spending' | null
  }

  let { label, pending, divider }: Props = $props()
</script>

<span class="mf-table__name">
  <span class="mf-table__text">{label}</span>
  {#if pending}<StatusDot status="stale" label="Pending edits" />{/if}
  {#if divider}<span class="mf-table__divider" aria-hidden="true">{divider}</span>{/if}
</span>
