<script lang="ts">
  import {
    Button,
    Checkbox,
    DateRangePicker,
    Modal,
    resolveRange,
    type RangeSelection,
  } from '@kenn-io/kit-ui'
  import { onMount, untrack } from 'svelte'

  import type { ViewProjection } from '../lib/api/client'
  import type { TransitionAction } from '../lib/controller/view-controller.svelte'

  interface Props {
    projection: ViewProjection
    onapply: (action: TransitionAction) => Promise<boolean>
    onclose: () => void
  }

  let { projection, onapply, onclose }: Props = $props()
  const initial = untrack(() => projection)
  let container: HTMLDivElement | undefined = $state()
  let showHidden = $state(initial.filters.show_hidden)
  let showTransfers = $state(initial.filters.show_transfers)
  let range = $state.raw<RangeSelection>(
    initial.filters.date_range
      ? {
          mode: 'custom',
          from: initial.filters.date_range.from,
          to: initial.filters.date_range.to,
        }
      : { mode: 'relative', days: 0 },
  )

  async function apply(): Promise<void> {
    const resolved = resolveRange(range)
    const dateRange = range.mode === 'relative' && range.days === 0 ? undefined : resolved
    const applied = await onapply({
      action: 'filters.apply',
      filters: {
        ...(dateRange === undefined
          ? {}
          : { date_range: { start: dateRange.from, end: dateRange.to } }),
        show_hidden: showHidden,
        show_transfers: showTransfers,
      },
    })
    if (applied) onclose()
  }

  onMount(() => container?.querySelector<HTMLButtonElement>('button')?.focus())
</script>

<Modal title="Filter transactions" closeLabel="Cancel filters" {onclose}>
  <div class="overlay-form" bind:this={container}>
    <p class="overlay-label">Inclusive date range</p>
    <DateRangePicker selection={range} onSelect={(next) => (range = next)} block />
    <Checkbox bind:checked={showHidden} label="Show hidden transactions" />
    <Checkbox bind:checked={showTransfers} label="Show transfers" />
  </div>
  {#snippet footer()}
    <Button onclick={onclose}>Cancel</Button>
    <Button tone="info" surface="solid" onclick={() => void apply()}>Apply filters</Button>
  {/snippet}
</Modal>
