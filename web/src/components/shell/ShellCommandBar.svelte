<script lang="ts">
  import { Button, IconButton, MEDIA, Toggle } from '@kenn-io/kit-ui'
  import SearchIcon from '@lucide/svelte/icons/search'
  import SlidersHorizontalIcon from '@lucide/svelte/icons/sliders-horizontal'
  import { onDestroy, tick } from 'svelte'
  import { MediaQuery } from 'svelte/reactivity'

  import Breadcrumbs from './Breadcrumbs.svelte'
  import BreakDownBy from './BreakDownBy.svelte'
  import CommandBar from './CommandBar.svelte'
  import CommandSearch from './CommandSearch.svelte'
  import CurrencyChip from './CurrencyChip.svelte'
  import FiltersChip from './FiltersChip.svelte'
  import PeriodControl from './PeriodControl.svelte'
  import PeriodSegments from './PeriodSegments.svelte'
  import ViewSheet from './ViewSheet.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { ViewController } from '../../lib/controller/view-controller.svelte'
  import type { ChartState } from '../../lib/shell/chart-state.svelte'
  import { drillFrames } from '../../lib/shell/drills'
  import type { OverlayState } from '../../lib/shell/overlay-state.svelte'
  import { SearchCarry } from '../../lib/shell/search-carry'
  import type { TransitionQueue } from '../../lib/shell/transition-queue'

  interface Props {
    controller: ViewController
    projection: ViewProjection
    overlays: OverlayState
    charts: ChartState
    /** Phone width: two rows; the view controls move to the View sheet, search behind an icon. */
    compact: boolean
    /** Orders view changes from the command bar after the ones the user made before them. */
    queue: TransitionQueue
    /** Returns focus to the grid; only `fromSearch` takes it from the search field. */
    onfocusgrid: (fromSearch?: boolean) => void
  }

  let { controller, projection, overlays, charts, compact, queue, onfocusgrid }: Props = $props()
  let search = $state<CommandSearch>()
  let filters = $state<FiltersChip>()
  let searchToggle = $state<HTMLElement>()
  /** Phone width: the search row was opened or has focus; it also shows while a search applies. */
  let searchShown = $state(false)
  const searchVisible = $derived(searchShown || (projection.view.search ?? '') !== '')
  /** The search row opened from its icon, so closing it returns there instead of the grid. */
  let searchFromIcon = false
  /** Whether the search row was open when the icon was pressed, before the press took focus. */
  let shownAtPress = false
  /** Where the sidebar shows its top bar, which is then the page's banner. */
  const narrow = new MediaQuery(MEDIA.wide)
  /** Keeps a search being typed when the window crosses the phone width and the field changes. */
  const searchCarry = new SearchCarry()
  onDestroy(() => searchCarry.destroy())

  /** Focuses the search field, for the `/` key; at phone width it reveals the field first. */
  export async function focusSearch(): Promise<void> {
    if (compact && !searchVisible) {
      searchShown = true
      await tick()
    }
    search?.focus()
  }

  /** Opens the Filters menu, for the `f` key; at phone width it lives in the View sheet. */
  export async function openFilters(): Promise<void> {
    if (compact) {
      overlays.open('view')
      await tick()
    }
    filters?.open()
  }

  async function backTo(depth: number): Promise<void> {
    await queue.apply({ action: 'view.back-to', depth })
    onfocusgrid()
  }

  // Pressing the icon moves focus out of the field, which folds the row before the click.
  function notePress(node: HTMLElement) {
    const press = () => (shownAtPress = searchShown)
    node.addEventListener('pointerdown', press)
    return () => node.removeEventListener('pointerdown', press)
  }

  function toggleSearch(): void {
    const shown = shownAtPress || searchShown
    shownAtPress = false
    searchFromIcon = !shown
    if (!shown) return void focusSearch()
    searchShown = false
  }

  // kit Button has no aria-haspopup prop; set it on its button.
  function opensDialog(node: HTMLElement): void {
    node.querySelector('button')?.setAttribute('aria-haspopup', 'dialog')
  }

  async function searchDone(): Promise<void> {
    const toIcon = compact && searchFromIcon
    searchShown = false
    searchFromIcon = false
    if (!toIcon) return onfocusgrid(true)
    await tick()
    searchToggle?.querySelector('button')?.focus()
  }

  // Tapping elsewhere commits the search (CommandSearch) and folds the row away unless the
  // search it leaves applied keeps it open.
  function searchLeft(event: FocusEvent & { currentTarget: HTMLElement }): void {
    const next = event.relatedTarget
    if (next instanceof Node && event.currentTarget.contains(next)) return
    if (!document.hasFocus()) return
    searchShown = false
    searchFromIcon = false
  }
</script>

{#snippet period()}
  <PeriodControl
    {projection}
    chooserOpen={overlays.current === 'period'}
    onchoose={() => overlays.open('period')}
    onclose={(focus) => overlays.close(focus)}
    onapply={queue.apply}
    segments={!compact}
  />
{/snippet}
{#snippet segments()}
  <PeriodSegments {projection} onapply={queue.apply} block />
{/snippet}
{#snippet currency()}
  <CurrencyChip
    {projection}
    onchange={(currency) => queue.apply({ action: 'view.set-currency', currency })}
  />
{/snippet}
{#snippet filtersChip()}
  <FiltersChip bind:this={filters} {projection} onapply={queue.apply} />
{/snippet}
{#snippet breakdown()}
  <BreakDownBy
    {projection}
    block={compact}
    onbreakdown={(dimension) => queue.apply({ action: 'view.set-subgrouping', dimension })}
    ontransactions={() =>
      queue.apply({ action: 'view.back-to', depth: drillFrames(projection).length })}
  />
{/snippet}
{#snippet searchField()}
  <CommandSearch
    bind:this={search}
    {controller}
    settled={queue.settled}
    carry={searchCarry}
    reveal={() => (searchShown = compact)}
    ondone={() => void searchDone()}
  />
{/snippet}
{#snippet chartsSwitch()}
  <Toggle
    class="mf-charts-switch"
    checked={charts.checked}
    onchange={(checked) => charts.toggle(checked)}
    label="Charts"
  />
{/snippet}

{#if compact}
  <CommandBar banner={!narrow.current}>
    {#snippet leading()}
      <Breadcrumbs {projection} onback={(depth) => void backTo(depth)} />
      {@render chartsSwitch()}
    {/snippet}
    {#snippet center()}
      {@render period()}
      <span class="mf-view-button-slot" {@attach opensDialog}>
        <Button
          class="mf-view-button"
          ariaExpanded={overlays.current === 'view'}
          onclick={(event) => overlays.open('view', event.currentTarget as HTMLElement)}
        >
          <SlidersHorizontalIcon size={14} aria-hidden="true" />View
        </Button>
      </span>
      <span class="mf-search-toggle" bind:this={searchToggle} {@attach notePress}>
        <IconButton
          ariaLabel="Search"
          title="Search · /"
          ariaPressed={searchVisible}
          ariaControls="mf-command-search-row"
          onclick={toggleSearch}
        >
          <SearchIcon size={18} aria-hidden="true" />
        </IconButton>
      </span>
    {/snippet}
    {#snippet below()}
      <div
        class="mf-command-bar__search"
        id="mf-command-search-row"
        hidden={!searchVisible}
        onfocusin={() => (searchShown = true)}
        onfocusout={searchLeft}
      >
        {@render searchField()}
      </div>
    {/snippet}
  </CommandBar>
  <ViewSheet
    {overlays}
    {projection}
    period={segments}
    {currency}
    filters={filtersChip}
    {breakdown}
  />
{:else}
  <CommandBar banner={!narrow.current}>
    {#snippet leading()}
      <Breadcrumbs {projection} onback={(depth) => void backTo(depth)} />
      {@render breakdown()}
    {/snippet}
    {#snippet center()}
      {@render period()}
      {@render currency()}
      {@render filtersChip()}
      {@render searchField()}
    {/snippet}
    {#snippet trailing()}
      {@render chartsSwitch()}
    {/snippet}
  </CommandBar>
{/if}
