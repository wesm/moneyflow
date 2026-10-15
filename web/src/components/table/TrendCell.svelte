<script lang="ts">
  import {
    END_RADIUS,
    sparkline,
    SPARK_HEIGHT,
    SPARK_WIDTH,
    type RowSeries,
  } from '../../lib/table/trend'

  interface Props {
    series: RowSeries
  }

  let { series }: Props = $props()
  const line = $derived(
    series.state === 'ready'
      ? sparkline(series.series.trend, series.response.ranges.trend)
      : undefined,
  )
</script>

<!-- A picture of the row's last twelve months; the row's own cells carry its numbers. -->
{#if line}
  <svg
    class="mf-trend"
    width={SPARK_WIDTH}
    height={SPARK_HEIGHT}
    viewBox={`0 0 ${SPARK_WIDTH} ${SPARK_HEIGHT}`}
    aria-hidden="true"
    focusable="false"
  >
    <line class="mf-trend__zero" x1="0" x2={SPARK_WIDTH} y1={line.zero} y2={line.zero} />
    {#each line.segments as points, index (index)}
      <polyline
        class="mf-trend__line"
        points={points.map((point) => `${point.x},${point.y}`).join(' ')}
      />
    {/each}
    {#if line.end}
      <circle class="mf-trend__end" cx={line.end.x} cy={line.end.y} r={END_RADIUS} />
    {/if}
  </svg>
{:else if series.state === 'loading'}
  <span class="mf-skeleton" aria-hidden="true"></span>
{/if}
