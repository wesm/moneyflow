<script lang="ts">
  import ChartRow from './ChartRow.svelte'
  import ChartStates from './ChartStates.svelte'
  import KpiStrip from './KpiStrip.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import { kpiTiles } from '../../lib/charts/kpis'
  import type { InsightsCache } from '../../lib/insights/cache.svelte'

  interface Props {
    /** The displayed view; the cache syncs to it. */
    projection: ViewProjection
    cache: InsightsCache
    /** Whether the chart row shows below the KPI strip. */
    chartsOn: boolean
    /** Receives this block's height whenever it resizes, so the table can keep its rows aligned. */
    measure?: ((height: number) => void) | undefined
  }

  let { projection, cache, chartsOn, measure }: Props = $props()
  let area = $state<HTMLElement>()
  const failed = $derived(cache.status === 'failed')
  const entry = $derived(failed ? undefined : cache.entry)
  const comparisonLabel = $derived(entry?.response.ranges.comparison_label)
  const tiles = $derived(entry && kpiTiles(entry, comparisonLabel))
  // Without a currency (every currency at once) there is nothing to chart.
  const shown = $derived(projection.view.currency !== undefined)

  $effect(() => {
    const element = area
    const report = measure
    if (!element || !report) return
    const observer = new ResizeObserver(() => report(element.offsetHeight))
    observer.observe(element)
    return () => {
      observer.disconnect()
      report(0)
    }
  })
</script>

<div class="mf-chart-area" bind:this={area}>
  {#if shown}
    <ChartStates
      failed={failed || cache.seriesFailed}
      stale={cache.stale}
      onretry={() => cache.retry()}
      content={failed ? undefined : charts}
    />
  {/if}
</div>

{#snippet charts()}
  <KpiStrip {tiles} {comparisonLabel} />
  {#if chartsOn}
    <ChartRow busy={entry === undefined} />
  {/if}
{/snippet}
