<script lang="ts">
  import type { Snippet } from 'svelte'

  import BottomSheet from './BottomSheet.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import { breakDownOptions } from '../../lib/shell/breadcrumbs'
  import type { OverlayState } from '../../lib/shell/overlay-state.svelte'

  /** The 'view' overlay. */
  interface Props {
    overlays: OverlayState
    projection: ViewProjection
    /** The command bar's own controls, rendered here at phone width. */
    period: Snippet
    currency: Snippet
    filters: Snippet
    breakdown: Snippet
  }

  let { overlays, projection, period, currency, filters, breakdown }: Props = $props()
  const multiCurrency = $derived(projection.currencies.length > 1)
  const drilled = $derived(breakDownOptions(projection).length > 0)
</script>

{#snippet row(label: string, control: Snippet)}
  <div class="mf-view-sheet__row">
    <p class="mf-view-sheet__label" aria-hidden="true">{label}</p>
    {@render control()}
  </div>
{/snippet}

<BottomSheet
  open={overlays.current === 'view'}
  title="View"
  {overlays}
  onclose={() => overlays.close()}
>
  <div class="mf-view-sheet">
    {@render row('Period', period)}
    {#if multiCurrency}{@render row('Currency', currency)}{/if}
    <div class="mf-view-sheet__row">{@render filters()}</div>
    {#if drilled}{@render row('Break down by', breakdown)}{/if}
  </div>
</BottomSheet>
