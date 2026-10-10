<script lang="ts">
  import type { KpiTile } from '../../lib/charts/kpis'

  interface Props {
    /** The tile to show, or none for a skeleton of the same size. */
    tile?: KpiTile | undefined
  }

  let { tile }: Props = $props()
</script>

{#if tile}
  <div class="mf-kpi">
    <dt class="mf-kpi__label">{tile.label}</dt>
    <dd class="mf-kpi__value">{tile.value}</dd>
    <dd class={['mf-kpi__note', tile.change && `mf-tone--${tile.change.tone}`]}>
      {#if tile.change}
        <span aria-hidden="true">{tile.change.text}</span>
        <span class="kit-sr-only">{tile.change.spoken}</span>
      {:else}
        {tile.detail ?? ''}
      {/if}
    </dd>
  </div>
{:else}
  <div class="mf-kpi" aria-hidden="true">
    <span class="mf-kpi__label"><span class="mf-skeleton"></span></span>
    <span class="mf-kpi__value"><span class="mf-skeleton"></span></span>
    <span class="mf-kpi__note"></span>
  </div>
{/if}
