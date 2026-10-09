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

  interface Props {
    projection: ViewProjection
    onapply: (action: TransitionAction) => void
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

  function choose(value: string): void {
    if (value === 'all') return onapply({ action: 'time.clear-period' })
    const anchor = anchorDate(range, currency?.latest)
    if (anchor) {
      onapply({ action: 'time.set-period', period: periodContaining(anchor, value as PeriodKind) })
    }
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
