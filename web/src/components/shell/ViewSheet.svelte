<script lang="ts">
  import type { Snippet } from 'svelte'

  import BottomSheet from './BottomSheet.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import { breakDownOptions } from '../../lib/shell/breadcrumbs'
  import { currencyChoices } from '../../lib/shell/period'

  interface Props {
    open: boolean
    onclose: () => void
    projection: ViewProjection
    /** The command bar's own controls, rendered here at phone width. */
    period: Snippet
    currency: Snippet
    filters: Snippet
    breakdown: Snippet
  }

  let { open, onclose, projection, period, currency, filters, breakdown }: Props = $props()
  const multiCurrency = $derived(currencyChoices(projection).length > 1)
  const drilled = $derived(breakDownOptions(projection).length > 0)
</script>

{#snippet row(label: string, control: Snippet)}
  <div class="mf-view-sheet__row">
    <p class="mf-view-sheet__label" aria-hidden="true">{label}</p>
    {@render control()}
  </div>
{/snippet}

<BottomSheet {open} title="View" {onclose}>
  <div class="mf-view-sheet">
    {@render row('Period', period)}
    {#if multiCurrency}{@render row('Currency', currency)}{/if}
    <div class="mf-view-sheet__row">{@render filters()}</div>
    {#if drilled}{@render row('Break down by', breakdown)}{/if}
  </div>
</BottomSheet>
