<script lang="ts">
  import CashflowChart from './CashflowChart.svelte'
  import ChartRow from './ChartRow.svelte'
  import ChartStates from './ChartStates.svelte'
  import KpiStrip from './KpiStrip.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import { cashflowCursor } from '../../lib/charts/cashflow'
  import { kpiTiles } from '../../lib/charts/kpis'
  import type { InsightsCache } from '../../lib/insights/cache.svelte'
  import { drilled } from '../../lib/shell/drills'
  import type { Period } from '../../lib/shell/period'

  interface Props {
    /** The displayed view; the cache syncs to it. */
    projection: ViewProjection
    cache: InsightsCache
    /** Whether the chart row shows below the KPI strip. */
    chartsOn: boolean
    /** The table's cursor row; the cash-flow chart draws its share solid. */
    cursorIndex: number
    /** Scopes the view to a chart period or range, through the transition queue. */
    onperiod: (period: Period) => void
    /** Esc in a chart returns to the table; absent in the phone dialog, which Esc closes. */
    onleave?: (() => void) | undefined
    /** Receives this block's height whenever it resizes, so the table can keep its rows aligned. */
    measure?: ((height: number) => void) | undefined
  }

  let { projection, cache, chartsOn, cursorIndex, onperiod, onleave, measure }: Props = $props()
  let area = $state<HTMLElement>()
  const failed = $derived(cache.status === 'failed')
  const entry = $derived(failed ? undefined : cache.entry)
  const comparisonLabel = $derived(entry?.response.ranges.comparison_label)
  const tiles = $derived(entry && kpiTiles(entry, comparisonLabel, drilled(entry.query)))
  // Reads cached series only, so moving the cursor never waits for the network.
  const cursor = $derived(
    entry &&
      cashflowCursor(projection, cursorIndex, entry, (identity) => cache.seriesFor(identity)),
  )
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

{#if shown}
  <div class="mf-chart-area" bind:this={area}>
    <ChartStates
      failed={failed || cache.seriesFailed}
      stale={cache.stale}
      onretry={() => cache.retry()}
      content={failed ? undefined : charts}
    />
  </div>
{/if}

{#snippet charts()}
  <KpiStrip {tiles} {comparisonLabel} />
  {#if chartsOn}
    <ChartRow busy={entry === undefined || cache.stale} cashflow={entry && cashflow} />
  {/if}
{/snippet}

{#snippet cashflow()}
  {#if entry}
    <CashflowChart {entry} {cursor} {onperiod} {onleave} />
  {/if}
{/snippet}
