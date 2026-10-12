<script lang="ts">
  import type { Snippet } from 'svelte'

  interface Props {
    /** The cash-flow chart, about two-thirds of the row. A skeleton holds its place until then. */
    cashflow?: Snippet | undefined
    /** The right panel; below the cash-flow chart under 900 px. */
    panel?: Snippet | undefined
    /** The insights these charts draw from are still loading. */
    busy: boolean
  }

  let { cashflow, panel, busy }: Props = $props()
</script>

<section class="mf-chart-row" aria-label="Charts" aria-busy={busy}>
  <div class="mf-chart-row__slot">
    {#if cashflow}
      {@render cashflow()}
    {:else}
      <div class="mf-skeleton mf-skeleton--fill" aria-hidden="true"></div>
    {/if}
  </div>
  <div class="mf-chart-row__slot">
    {#if panel}
      {@render panel()}
    {:else}
      <div class="mf-skeleton mf-skeleton--fill" aria-hidden="true"></div>
    {/if}
  </div>
</section>
