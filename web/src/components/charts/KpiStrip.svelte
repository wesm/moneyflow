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
    <!-- Each change already says what it compares with, so this line is for sighted readers. -->
    <p class="mf-kpis__caption" aria-hidden="true">{comparisonLabel ?? ''}</p>
  </div>
{:else}
  <div class="mf-kpis">
    <div class="mf-kpis__tiles">
      {#each { length: 5 }, index (index)}
        <KpiTile />
      {/each}
    </div>
    <p class="mf-kpis__caption" aria-hidden="true"></p>
  </div>
{/if}
