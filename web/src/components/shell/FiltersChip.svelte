<script lang="ts">
  import { FilterDropdown } from '@kenn-io/kit-ui'

  import type { ViewProjection } from '../../lib/api/client'
  import type { TransitionAction } from '../../lib/controller/view-controller.svelte'
  import { containMenuKeys } from '../../lib/shell/menu-keys'

  interface Props {
    projection: ViewProjection
    onapply: (action: TransitionAction) => Promise<unknown>
  }

  interface Toggles {
    show_hidden: boolean
    show_transfers: boolean
  }

  // The server's defaults (internal/api/viewstate.go): a link names only filters changed from them.
  const defaultShowHidden = true
  const defaultShowTransfers = false

  let { projection, onapply }: Props = $props()
  let wrapper = $state<HTMLDivElement>()
  /** Toggles sent but not yet answered, so a quick second click builds on the first. */
  let pending = $state.raw<Toggles>()
  const filters = $derived(projection.filters)
  const shown = $derived<Toggles>(
    pending ?? { show_hidden: filters.show_hidden, show_transfers: filters.show_transfers },
  )
  const changed = $derived(
    Number(shown.show_hidden !== defaultShowHidden) +
      Number(shown.show_transfers !== defaultShowTransfers),
  )

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
  async function apply(change: Partial<Toggles>): Promise<void> {
    const next = { ...shown, ...change }
    const range = filters.date_range
    pending = next
    try {
      await onapply({
        action: 'filters.apply',
        filters: {
          ...(range ? { date_range: { start: range.from, end: range.to } } : {}),
          ...next,
        },
      })
    } finally {
      if (pending === next) pending = undefined
    }
  }

  const sections = $derived([
    {
      items: [
        {
          id: 'hidden',
          label: 'Show hidden transactions',
          active: shown.show_hidden,
          onSelect: () => void apply({ show_hidden: !shown.show_hidden }),
        },
        {
          id: 'transfers',
          label: 'Show transfers',
          active: shown.show_transfers,
          onSelect: () => void apply({ show_transfers: !shown.show_transfers }),
        },
      ],
    },
  ])
</script>

<div class="mf-filters-chip" bind:this={wrapper} {@attach containMenuKeys}>
  <FilterDropdown
    label="Filters"
    title="Filters · f"
    badgeCount={changed}
    {sections}
    align="end"
    minWidth="220px"
  />
</div>
