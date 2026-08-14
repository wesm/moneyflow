<script lang="ts">
  import { Axis, Bars, Chart, Highlight, Layer, Tooltip } from 'layerchart'

  import type { ChartMark, ChartPartition } from '../lib/chart'

  interface Props {
    partitions: readonly ChartPartition[]
    cursorIndex: number
    oncursor: (index: number) => void
    ondrill: (identity: string) => void
  }
  let { partitions, cursorIndex, oncursor, ondrill }: Props = $props()

  function activate(event: KeyboardEvent, mark: ChartMark): void {
    if (event.key !== 'Enter') return
    event.preventDefault()
    event.stopPropagation()
    ondrill(mark.identity)
  }
</script>

<p class="chart-description">Aggregate totals by current table order.</p>
{#each partitions as partition (partition.key)}
  <section class="chart-partition" aria-label={`${partition.currency} chart`}>
    <h3>{partition.currency} · {partition.scale} decimal places</h3>
    <div class="layer-chart" aria-hidden="true">
      <Chart
        data={partition.marks}
        x="ratio"
        y="categoricalKey"
        valueAxis="x"
        width={320}
        height={Math.max(120, partition.marks.length * 34)}
        padding={{ top: 8, right: 12, bottom: 28, left: 84 }}
        motion="none"
        tooltipContext={{ mode: 'band' }}
      >
        {#snippet children({ context })}
          <Layer type="svg">
            <Axis placement="bottom" />
            <Bars
              fill="currentColor"
              stroke="currentColor"
              strokeWidth={1}
              radius={3}
              key={(mark: ChartMark) => mark.identity}
              onBarClick={(_event: MouseEvent, detail: { data: ChartMark }) =>
                oncursor(detail.data.index)}
            />
            <Highlight
              data={partition.marks.find((mark) => mark.index === cursorIndex)}
              bar
              motion="none"
            />
          </Layer>
          <Tooltip.Root {context} fadeDuration={0} motion="none" portal={false}>
            {#snippet children({ data }: { data: ChartMark })}
              <Tooltip.Item label={data.label} value={data.display} />
            {/snippet}
          </Tooltip.Root>
        {/snippet}
      </Chart>
    </div>
    <div class="chart-marks" aria-label={`${partition.currency} chart marks`}>
      {#each partition.marks as mark (mark.identity)}
        <button
          class="chart-mark"
          class:chart-mark--active={mark.index === cursorIndex}
          type="button"
          aria-label={`${mark.label}, ${mark.display}`}
          aria-pressed={mark.index === cursorIndex}
          onclick={() => oncursor(mark.index)}
          ondblclick={() => ondrill(mark.identity)}
          onkeydown={(event) => activate(event, mark)}
        >
          <span>{mark.label}</span><span>{mark.display}</span>
        </button>
      {/each}
    </div>
  </section>
{/each}
