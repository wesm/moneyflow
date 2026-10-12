<script lang="ts">
  import { SegmentedControl } from '@kenn-io/kit-ui'

  import type { ViewProjection } from '../../lib/api/client'
  import type { TransitionAction } from '../../lib/controller/view-controller.svelte'
  import {
    activeCurrency,
    anchorDate,
    periodContaining,
    scopeKind,
    type PeriodKind,
  } from '../../lib/shell/period'
  import type { ViewChange } from '../../lib/shell/transition-queue'

  interface Props {
    projection: ViewProjection
    onapply: (change: ViewChange) => void
    /** Fills the row, as in the View sheet. */
    block?: boolean
  }

  let { projection, onapply, block = false }: Props = $props()

  const range = $derived(projection.filters.date_range)
  const kind = $derived(scopeKind(range))
  const currency = $derived(activeCurrency(projection))
  const segments = [
    { value: 'day', label: 'Day' },
    { value: 'month', label: 'Month' },
    { value: 'year', label: 'Year' },
    { value: 'all', label: 'All' },
  ]

  // The period is anchored on the view shown when the change runs, after any pending one.
  function choose(value: string): void {
    if (value === 'all') return onapply({ action: 'time.clear-period' })
    onapply((current) => periodOf(current, value as PeriodKind))
  }

  function periodOf(current: ViewProjection, kind: PeriodKind): TransitionAction | undefined {
    const anchor = anchorDate(current.filters.date_range, activeCurrency(current)?.latest)
    return anchor
      ? { action: 'time.set-period', period: periodContaining(anchor, kind) }
      : undefined
  }
</script>

<SegmentedControl
  ariaLabel="Period"
  {block}
  options={segments}
  value={kind === 'custom' ? '' : kind}
  disabled={!currency}
  onchange={choose}
/>
