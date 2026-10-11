<script lang="ts">
  import CalendarHeatmap from './CalendarHeatmap.svelte'
  import CashflowChart from './CashflowChart.svelte'
  import ChartRow from './ChartRow.svelte'
  import ChartStates from './ChartStates.svelte'
  import CompositionPanel from './CompositionPanel.svelte'
  import KpiStrip from './KpiStrip.svelte'
  import PeriodComposition from './PeriodComposition.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import { cashflowCursor } from '../../lib/charts/cashflow'
  import { kpiTiles } from '../../lib/charts/kpis'
  import type { InsightsCache } from '../../lib/insights/cache.svelte'
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
    /** Moves the table cursor to the aggregate row a composition bar stands for. */
    onrow?: ((identity: string) => void) | undefined
    /** Drills into the aggregate row a composition bar stands for. */
    ondrill?: ((identity: string) => void) | undefined
    /** Receives this block's height whenever it resizes, so the table can keep its rows aligned. */
    measure?: ((height: number) => void) | undefined
  }

  let {
    projection,
    cache,
    chartsOn,
    cursorIndex,
    onperiod,
    onleave,
    onrow,
    ondrill,
    measure,
  }: Props = $props()
  let area = $state<HTMLElement>()
  const failed = $derived(cache.status === 'failed')
  const entry = $derived(failed ? undefined : cache.entry)
  const comparisonLabel = $derived(entry?.response.ranges.comparison_label)
  const tiles = $derived(entry && kpiTiles(entry, comparisonLabel))
  // Reads cached series only, so moving the cursor never waits for the network.
  const cursor = $derived(
    entry &&
      cashflowCursor(projection, cursorIndex, entry, (identity) => cache.seriesFor(identity)),
  )
  const cursorRow = $derived(projection.aggregate_rows?.find((row) => row.index === cursorIndex))
  const cursorDate = $derived(
    projection.detail_rows?.find((row) => row.index === cursorIndex)?.date,
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
    <ChartRow
      busy={entry === undefined || cache.stale}
      cashflow={entry && cashflow}
      panel={entry && panel}
    />
  {/if}
{/snippet}

{#snippet cashflow()}
  <!-- A cash flow the chart cannot draw shows the failure line; the rest keeps working. -->
  <svelte:boundary>
    {#if entry}
      <CashflowChart {entry} {cursor} {onperiod} {onleave} />
    {/if}
    <!-- eslint-disable-next-line @typescript-eslint/no-unused-vars -- the error comes first -->
    {#snippet failed(_error, reset)}
      <ChartStates failed stale={false} onretry={reset} />
    {/snippet}
  </svelte:boundary>
{/snippet}

{#snippet panel()}
  <!-- Like the cash flow, a panel the data cannot draw shows the failure line in its place. -->
  <svelte:boundary>
    {#if entry}
      {@const summary = entry.response.summary}
      {#if summary?.calendar}
        <CalendarHeatmap {entry} {cursorDate} {onperiod} {onleave} />
      {:else if summary?.period_composition}
        <PeriodComposition {entry} period={cursorRow?.period} {onleave} />
      {:else if summary?.composition}
        <CompositionPanel
          title="Where it went"
          composition={summary.composition}
          scale={entry.response.scale}
          cursorIdentity={cursorRow?.identity}
          {onrow}
          {ondrill}
          {onleave}
        />
      {/if}
    {/if}
    <!-- eslint-disable-next-line @typescript-eslint/no-unused-vars -- the error comes first -->
    {#snippet failed(_error, reset)}
      <ChartStates failed stale={false} onretry={reset} />
    {/snippet}
  </svelte:boundary>
{/snippet}
