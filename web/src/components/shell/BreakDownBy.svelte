<script lang="ts">
  import { SegmentedControl } from '@kenn-io/kit-ui'

  import type { ViewProjection } from '../../lib/api/client'
  import { breakDownOptions } from '../../lib/shell/breadcrumbs'
  import type { Dimension } from '../../lib/shortcuts'

  interface Props {
    projection: ViewProjection
    /** Breaks the drilled rows down by a dimension. */
    onbreakdown: (dimension: Dimension) => void
    /** Undoes the breakdown and shows the drilled transactions. */
    ontransactions: () => void
  }

  let { projection, onbreakdown, ontransactions }: Props = $props()
  const options = $derived(breakDownOptions(projection))

  function choose(value: string): void {
    if (value === 'transactions') ontransactions()
    else onbreakdown(value as Dimension)
  }
</script>

{#if options.length > 0}
  <div class="mf-break-down">
    <span class="mf-break-down__label" aria-hidden="true">Break down by</span>
    <SegmentedControl
      ariaLabel="Break down by"
      {options}
      value={projection.view.sub_grouping ?? 'transactions'}
      onchange={choose}
    />
  </div>
{/if}
