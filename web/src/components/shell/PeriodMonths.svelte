<script lang="ts">
  import { IconButton } from '@kenn-io/kit-ui'
  import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'

  import PeriodGrid from './PeriodGrid.svelte'
  import { monthCells, type DateRange } from '../../lib/shell/period'

  interface Props {
    /** The year whose months show; ‹ and › change it (bindable). */
    year: number
    range: DateRange
    first: string
    latest: string
    /** Months that take focus when none is selected, in order of preference. */
    starts: string[]
    onpick: (date: string) => void
  }

  let { year = $bindable(), range, first, latest, starts, onpick }: Props = $props()
</script>

<div class="mf-period-chooser__stepper">
  <IconButton
    ariaLabel="Previous year"
    size="sm"
    disabled={year <= Number(first.slice(0, 4))}
    onclick={() => (year -= 1)}
  >
    <ChevronLeftIcon size={14} aria-hidden="true" />
  </IconButton>
  <span aria-live="polite">{year}</span>
  <IconButton
    ariaLabel="Next year"
    size="sm"
    disabled={year >= Number(latest.slice(0, 4))}
    onclick={() => (year += 1)}
  >
    <ChevronRightIcon size={14} aria-hidden="true" />
  </IconButton>
</div>
<PeriodGrid
  label="Months of {year}"
  columns={4}
  cells={monthCells(year, range, first, latest)}
  {starts}
  {onpick}
/>
