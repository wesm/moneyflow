<script lang="ts">
  import CompositionPanel from './CompositionPanel.svelte'
  import type { InsightsResponse } from '../../lib/api/client'
  import { periodShare } from '../../lib/charts/composition'
  import type { InsightsEntry } from '../../lib/insights/cache.svelte'

  interface Props {
    entry: InsightsEntry
    /** The cursor row's period; its category-group shares show. */
    period?: InsightsResponse['ranges']['context'][number]['period'] | undefined
  }

  let { entry, period }: Props = $props()
  // Every period's shares arrived with the summary, so the cursor never waits for them.
  const share = $derived(periodShare(entry.response, period))
</script>

{#if share}
  <CompositionPanel
    title={`${share.label} by group`}
    composition={share.composition}
    scale={entry.response.scale}
  />
{/if}
