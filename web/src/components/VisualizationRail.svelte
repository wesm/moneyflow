<script lang="ts">
  import { EmptyState } from '@kenn-io/kit-ui'
  import { SvelteMap } from 'svelte/reactivity'

  import AggregateBars from './AggregateBars.svelte'
  import DetailSummary from './DetailSummary.svelte'
  import TimeBars from './TimeBars.svelte'
  import { partitionChartMarks, type ServerPeriod } from '../lib/chart'
  import type { ViewProjection } from '../lib/api/client'

  interface Props {
    projection: ViewProjection
    cursorIndex: number
    oncursor: (index: number) => void
    ondrill: (identity: string) => void
  }
  let { projection, cursorIndex, oncursor, ondrill }: Props = $props()
  const detail = $derived(Array.isArray(projection.detail_rows))
  const time = $derived(projection.view.grouping === 'time')
  const periods = $derived.by(() => {
    const result = new SvelteMap<string, ServerPeriod>()
    for (const row of projection.aggregate_rows ?? []) {
      if (row.period) result.set(row.identity, row.period)
    }
    return result
  })
  const partitions = $derived(partitionChartMarks(projection.chart.marks ?? [], periods, time))
  const hasChart = $derived(
    detail ? (projection.chart.summary?.length ?? 0) > 0 : partitions.length > 0,
  )
</script>

<div class="visualization-rail">
  <h2>Visualizations</h2>
  {#if !hasChart}
    <EmptyState title="No chart data" description="The table remains the complete source view." />
  {:else if detail}
    <DetailSummary summary={projection.chart.summary ?? []} />
  {:else if time}
    <TimeBars {partitions} {cursorIndex} {oncursor} {ondrill} />
  {:else}
    <AggregateBars {partitions} {cursorIndex} {oncursor} {ondrill} />
  {/if}
</div>
