<script lang="ts">
  import { IconButton, SegmentedControl } from '@kenn-io/kit-ui'
  import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'

  import PeriodChooser from './PeriodChooser.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { TransitionAction } from '../../lib/controller/view-controller.svelte'
  import {
    activeCurrency,
    anchorDate,
    canShiftPeriod,
    periodContaining,
    periodLabel,
    scopeKind,
    type Period,
    type PeriodKind,
  } from '../../lib/shell/period'

  interface Props {
    projection: ViewProjection
    chooserOpen: boolean
    onchoose: () => void
    /** Closes the chooser; 'keep' leaves focus where a pointer press put it. */
    onclose: (focus?: 'keep') => void
    onapply: (action: TransitionAction) => void
  }

  let { projection, chooserOpen, onchoose, onclose, onapply }: Props = $props()
  let labelButton = $state<HTMLButtonElement>()

  const range = $derived(projection.filters.date_range)
  const kind = $derived(scopeKind(range))
  const currency = $derived(activeCurrency(projection))
  const segments = [
    { value: 'day', label: 'Day' },
    { value: 'month', label: 'Month' },
    { value: 'year', label: 'Year' },
    { value: 'all', label: 'All' },
  ]

  function setPeriod(period: Period | undefined): void {
    onapply(period ? { action: 'time.set-period', period } : { action: 'time.clear-period' })
  }

  function chooseSegment(value: string): void {
    if (value === 'all') return setPeriod(undefined)
    const anchor = anchorDate(range, currency?.latest)
    if (anchor) setPeriod(periodContaining(anchor, value as PeriodKind))
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
    title="Choose period · t"
    disabled={!currency}
    bind:this={labelButton}
    onclick={() => (chooserOpen ? onclose() : onchoose())}
  >
    {periodLabel(range)}
  </button>
  <IconButton
    ariaLabel="Next period"
    size="sm"
    disabled={!canShiftPeriod(projection)}
    onclick={() => onapply({ action: 'time.next-period' })}
  >
    <ChevronRightIcon size={14} aria-hidden="true" />
  </IconButton>
  <SegmentedControl
    ariaLabel="Period"
    options={segments}
    value={kind === 'custom' ? '' : kind}
    disabled={!currency}
    onchange={chooseSegment}
  />
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
