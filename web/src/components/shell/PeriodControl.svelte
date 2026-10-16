<script lang="ts">
  import { IconButton, KbdBadge } from '@kenn-io/kit-ui'
  import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'

  import PeriodChooser from './PeriodChooser.svelte'
  import PeriodSegments from './PeriodSegments.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import { drillFrames } from '../../lib/shell/drills'
  import {
    activeCurrency,
    canShiftPeriod,
    periodLabel,
    scopeKind,
    type Period,
  } from '../../lib/shell/period'
  import type { ViewChange } from '../../lib/shell/transition-queue'

  interface Props {
    projection: ViewProjection
    chooserOpen: boolean
    onchoose: () => void
    /** Closes the chooser; 'keep' leaves focus where a pointer press put it. */
    onclose: (focus?: 'keep') => void
    onapply: (change: ViewChange) => void
    /** Shows the Day, Month, Year, and All segments; at phone width they move to the View sheet. */
    segments?: boolean
  }

  let { projection, chooserOpen, onchoose, onclose, onapply, segments = true }: Props = $props()
  let labelButton = $state<HTMLButtonElement>()

  const range = $derived(projection.filters.date_range)
  const currency = $derived(activeCurrency(projection))
  const label = $derived(periodLabel(range))

  // All time also clears a time drill, which can be open while the view covers all time. It is
  // decided on the view shown when the change runs, after any pending period change.
  function pick(period: Period | undefined): void {
    onclose()
    if (period) return onapply({ action: 'time.set-period', period })
    onapply((current) => {
      const timeDrill = drillFrames(current).some((frame) => frame.dimension === 'time')
      const all = scopeKind(current.filters.date_range) === 'all'
      return all && !timeDrill ? undefined : { action: 'time.clear-period' }
    })
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
    aria-keyshortcuts="p"
    title="{label} · Choose period · p"
    disabled={!currency}
    bind:this={labelButton}
    onclick={() => (chooserOpen ? onclose() : onchoose())}
  >
    <span class="mf-period__text">{label}</span>
    <span class="mf-period__key" aria-hidden="true"><KbdBadge keys={['p']} /></span>
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
