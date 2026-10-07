<script lang="ts">
  import type { ViewProjection } from '../lib/api/client'

  type Summary = NonNullable<ViewProjection['chart']['summary']>[number]
  interface Props {
    summary: readonly Summary[]
  }
  let { summary }: Props = $props()
</script>

{#each summary as partition (`${partition.currency}:${partition.scale}`)}
  <section class="chart-partition" aria-label={`${partition.currency} summary`}>
    <h3>{partition.currency}</h3>
    <dl class="detail-summary">
      <div>
        <dt>Income</dt>
        <dd>{partition.in.display}</dd>
      </div>
      <div>
        <dt>Outflow</dt>
        <dd>{partition.out.display}</dd>
      </div>
      <div>
        <dt>Net</dt>
        <dd>{partition.net.display}</dd>
      </div>
    </dl>
  </section>
{/each}
