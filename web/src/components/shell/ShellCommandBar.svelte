<script lang="ts">
  import { Toggle } from '@kenn-io/kit-ui'

  import Breadcrumbs from './Breadcrumbs.svelte'
  import BreakDownBy from './BreakDownBy.svelte'
  import CommandBar from './CommandBar.svelte'
  import CommandSearch from './CommandSearch.svelte'
  import CurrencyChip from './CurrencyChip.svelte'
  import FiltersChip from './FiltersChip.svelte'
  import PeriodControl from './PeriodControl.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type {
    TransitionAction,
    ViewController,
  } from '../../lib/controller/view-controller.svelte'
  import type { ChartState } from '../../lib/shell/chart-state.svelte'
  import { drillFrames } from '../../lib/shell/drills'
  import type { OverlayState } from '../../lib/shell/overlay-state.svelte'

  interface Props {
    controller: ViewController
    projection: ViewProjection
    overlays: OverlayState
    charts: ChartState
    /** Returns focus to the grid after a control moved the view. */
    onfocusgrid: () => void
  }

  let { controller, projection, overlays, charts, onfocusgrid }: Props = $props()
  let search = $state<CommandSearch>()
  let filters = $state<FiltersChip>()

  /** Focuses the search field, for the `/` key. */
  export function focusSearch(): void {
    search?.focus()
  }

  /** Opens the Filters menu, for the `f` key. */
  export function openFilters(): void {
    filters?.open()
  }

  function apply(action: TransitionAction): void {
    void controller.apply(action)
  }

  async function backTo(depth: number): Promise<void> {
    await controller.apply({ action: 'view.back-to', depth })
    onfocusgrid()
  }
</script>

<CommandBar>
  {#snippet leading()}
    <Breadcrumbs {projection} onback={(depth) => void backTo(depth)} />
    <BreakDownBy
      {projection}
      onbreakdown={(dimension) => apply({ action: 'view.set-subgrouping', dimension })}
      ontransactions={() =>
        apply({ action: 'view.back-to', depth: drillFrames(projection).length })}
    />
  {/snippet}
  {#snippet center()}
    <PeriodControl
      {projection}
      chooserOpen={overlays.current === 'period'}
      onchoose={() => overlays.open('period')}
      onclose={(focus) => overlays.close(focus)}
      onapply={apply}
    />
    <CurrencyChip
      {projection}
      onchange={(currency) => apply({ action: 'view.set-currency', currency })}
    />
    <FiltersChip bind:this={filters} {projection} onapply={(action) => controller.apply(action)} />
    <CommandSearch bind:this={search} {controller} ondone={onfocusgrid} />
  {/snippet}
  {#snippet trailing()}
    <Toggle
      checked={charts.checked}
      onchange={(checked) => charts.toggle(checked)}
      label="Charts"
    />
  {/snippet}
</CommandBar>
