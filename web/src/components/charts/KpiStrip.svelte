<script lang="ts">
  import KpiTile from './KpiTile.svelte'
  import type { KpiTile as Tile } from '../../lib/charts/kpis'

  interface Props {
    /** The tiles, or none while the first insights load. */
    tiles: readonly Tile[] | undefined
    /** The comparison every change is against, such as `vs Aug`; none for All. */
    comparisonLabel: string | undefined
  }

  let { tiles, comparisonLabel }: Props = $props()
</script>

{#if tiles}
  <div class="mf-kpis">
    <dl class="mf-kpis__tiles">
      {#each tiles as tile (tile.key)}
        <KpiTile {tile} />
      {/each}
    </dl>
    {#if comparisonLabel}
      <p class="mf-kpis__caption">{comparisonLabel}</p>
    {/if}
  </div>
{:else}
  <div class="mf-kpis">
    <div class="mf-kpis__tiles">
      {#each { length: 5 }, index (index)}
        <KpiTile />
      {/each}
    </div>
  </div>
{/if}
