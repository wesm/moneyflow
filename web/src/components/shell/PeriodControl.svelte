<script lang="ts">
  import { IconButton } from '@kenn-io/kit-ui'
  import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'

  import PeriodChooser from './PeriodChooser.svelte'
  import PeriodSegments from './PeriodSegments.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { TransitionAction } from '../../lib/controller/view-controller.svelte'
  import {
    activeCurrency,
    canShiftPeriod,
    periodLabel,
    scopeKind,
    type Period,
  } from '../../lib/shell/period'

  interface Props {
    projection: ViewProjection
    chooserOpen: boolean
    onchoose: () => void
    /** Closes the chooser; 'keep' leaves focus where a pointer press put it. */
    onclose: (focus?: 'keep') => void
    onapply: (action: TransitionAction) => void
    /** Shows the Day, Month, Year, and All segments; at phone width they move to the View sheet. */
    segments?: boolean
  }

  let { projection, chooserOpen, onchoose, onclose, onapply, segments = true }: Props = $props()
  let labelButton = $state<HTMLButtonElement>()

  const range = $derived(projection.filters.date_range)
  const kind = $derived(scopeKind(range))
  const currency = $derived(activeCurrency(projection))
  const label = $derived(periodLabel(range))

  function setPeriod(period: Period | undefined): void {
    onapply(period ? { action: 'time.set-period', period } : { action: 'time.clear-period' })
  }

  function pick(period: Period | undefined): void {
    onclose()
    if (period || kind !== 'all') setPeriod(period)
  }
</script>

<div class="mf-period">
  <IconButton
    ariaLabel="Previous period"
    size="sm"
    disabled={!canShiftPeriod(projection)}
    onclick={() => onapply({ action: 'time.previous-period' })}
  >
    <ChevronLeftIcon size={14} aria-hidden="true" />
  </IconButton>
  <button
    class="mf-period__label"
    type="button"
    aria-haspopup="dialog"
    aria-expanded={chooserOpen}
    aria-keyshortcuts="t"
    title="{label} · Choose period · t"
    disabled={!currency}
    bind:this={labelButton}
    onclick={() => (chooserOpen ? onclose() : onchoose())}
  >
    {label}
  </button>
  <IconButton
    ariaLabel="Next period"
    size="sm"
    disabled={!canShiftPeriod(projection)}
    onclick={() => onapply({ action: 'time.next-period' })}
  >
    <ChevronRightIcon size={14} aria-hidden="true" />
  </IconButton>
  {#if segments}<PeriodSegments {projection} {onapply} />{/if}
</div>
{#if chooserOpen && labelButton && currency}
  <PeriodChooser
    anchor={labelButton}
    {range}
    first={currency.first}
    latest={currency.latest}
    onpick={pick}
    ondismiss={() => onclose('keep')}
  />
{/if}
