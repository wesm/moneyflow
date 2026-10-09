<script lang="ts">
  import { FilterDropdown } from '@kenn-io/kit-ui'

  import type { ViewProjection } from '../../lib/api/client'
  import type { TransitionAction } from '../../lib/controller/view-controller.svelte'

  interface Props {
    projection: ViewProjection
    onapply: (action: TransitionAction) => void
  }

  let { projection, onapply }: Props = $props()
  let wrapper = $state<HTMLDivElement>()
  const filters = $derived(projection.filters)

  /**
   * Opens the menu for the `f` key. kit FilterDropdown has no programmatic open, so this
   * focuses and presses its trigger.
   */
  export function open(): void {
    const trigger = wrapper?.querySelector<HTMLButtonElement>('button')
    trigger?.focus()
    trigger?.click()
  }

  // Filters apply replaces the date range too, so the current period goes with every toggle.
  function apply(change: { show_hidden?: boolean; show_transfers?: boolean }): void {
    const range = filters.date_range
    onapply({
      action: 'filters.apply',
      filters: {
        ...(range ? { date_range: { start: range.from, end: range.to } } : {}),
        show_hidden: change.show_hidden ?? filters.show_hidden,
        show_transfers: change.show_transfers ?? filters.show_transfers,
      },
    })
  }

  const sections = $derived([
    {
      items: [
        {
          id: 'hidden',
          label: 'Show hidden transactions',
          active: filters.show_hidden,
          onSelect: () => apply({ show_hidden: !filters.show_hidden }),
        },
        {
          id: 'transfers',
          label: 'Show transfers',
          active: filters.show_transfers,
          onSelect: () => apply({ show_transfers: !filters.show_transfers }),
        },
      ],
    },
  ])
  const enabled = $derived(Number(filters.show_hidden) + Number(filters.show_transfers))
</script>

<div class="mf-filters-chip" bind:this={wrapper}>
  <FilterDropdown
    label="Filters"
    title="Filters · f"
    badgeCount={enabled}
    {sections}
    align="end"
    minWidth="220px"
  />
</div>
