<script lang="ts">
  import { priorChange } from '../../lib/charts/kpis'
  import type { RowSeries } from '../../lib/table/trend'

  interface Props {
    series: RowSeries
  }

  let { series }: Props = $props()
  const change = $derived.by(() => {
    if (series.state !== 'ready' || series.series.vs_prior === undefined) return undefined
    const { ranges, scale } = series.response
    return priorChange(series.series.vs_prior, scale, ranges.comparison_label ?? 'vs prior')
  })
</script>

{#if change}
  <span class={`mf-tone--${change.tone}`}>
    <span aria-hidden="true">{change.text}</span>
    <span class="kit-sr-only">{change.spoken}</span>
  </span>
{:else if series.state === 'loading'}
  <span class="mf-skeleton" aria-hidden="true"></span>
{/if}
