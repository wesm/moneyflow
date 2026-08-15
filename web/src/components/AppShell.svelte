<script lang="ts">
  import { DetailDrawer, MEDIA, StatusBar, ThemeToggle, Toggle, TopBar } from '@kenn-io/kit-ui'
  import { MediaQuery } from 'svelte/reactivity'
  import { onMount, tick } from 'svelte'
  import FiltersDialog from './FiltersDialog.svelte'
  import FinanceTable from './FinanceTable.svelte'
  import HelpDialog from './HelpDialog.svelte'
  import RefinementBar from './RefinementBar.svelte'
  import SearchOverlay from './SearchOverlay.svelte'
  import VisualizationRail from './VisualizationRail.svelte'
  import CategoryDialog from './editing/CategoryDialog.svelte'
  import CategoryManager from './editing/CategoryManager.svelte'
  import GroupManager from './editing/GroupManager.svelte'
  import MerchantDialog from './editing/MerchantDialog.svelte'
  import PendingStatus from './editing/PendingStatus.svelte'
  import ReviewDrawer from './editing/ReviewDrawer.svelte'
  import {
    createMoneyflowShortcuts,
    handleMoneyflowKeydown,
    type LocalAction,
  } from '../lib/shortcuts'
  import type { ViewController } from '../lib/controller/view-controller.svelte'
  interface Props {
    controller: ViewController
  }
  let { controller }: Props = $props()
  let charts = $state(true)
  let chartDrawer = $state(false)
  let grid = $state<HTMLElement | undefined>()
  type Overlay =
    | 'search'
    | 'filters'
    | 'help'
    | 'merchant'
    | 'category'
    | 'categories'
    | 'groups'
    | 'review'
  let overlay = $state<Overlay | undefined>()
  let popOverlayScope: (() => void) | undefined
  let popChartScope: (() => void) | undefined
  let shortcuts: ReturnType<typeof createMoneyflowShortcuts> | undefined
  const projection = $derived(controller.projection)
  const compact = new MediaQuery(MEDIA.compact)

  function local(action: LocalAction): void {
    if (action === 'cursor.up') void controller.moveCursor(-1)
    else if (action === 'cursor.down') void controller.moveCursor(1)
    else if (action === 'cursor.home') void controller.moveHome()
    else if (action === 'overlay.search') openOverlay('search')
    else if (action === 'overlay.filters') openOverlay('filters')
    else if (action === 'overlay.help') openOverlay('help')
    else if (action === 'edit.merchant') openOverlay('merchant')
    else if (action === 'edit.category') openOverlay('category')
    else if (action === 'manage.categories') openOverlay('categories')
    else if (action === 'manage.groups') openOverlay('groups')
    else if (action === 'edit.review') openOverlay('review')
    else if (action === 'edit.undo') void controller.editing.undo()
    else if (action === 'edit.redo') void controller.editing.redo()
    else if (action === 'edit.hide') {
      const row = focusedRow()
      if (row) {
        void controller.editing.submit({
          action: 'transaction.toggle-hidden',
          input: {},
          target: row,
        })
      }
    }
  }
  function focusedRow(): { identity: string; kind: 'detail' | 'aggregate' } | undefined {
    const row = [...(projection?.detail_rows ?? []), ...(projection?.aggregate_rows ?? [])].find(
      (candidate) => candidate.index === controller.cursorIndex,
    )
    return row
      ? { identity: row.identity, kind: projection?.detail_rows ? 'detail' : 'aggregate' }
      : undefined
  }
  async function apply(action: string): Promise<void> {
    if (action === 'view.drill' || action === 'selection.toggle') {
      const row = [...(projection?.detail_rows ?? []), ...(projection?.aggregate_rows ?? [])].find(
        (candidate) => candidate.index === controller.cursorIndex,
      )
      if (row) {
        await target(action, row.identity, projection?.detail_rows ? 'detail' : 'aggregate')
        return
      }
    }
    await controller.apply({ action })
    focusGrid()
  }
  async function target(
    action: string,
    identity: string,
    kind: 'detail' | 'aggregate',
  ): Promise<void> {
    await controller.apply({ action, target: { identity, kind } })
    focusGrid()
  }
  function openOverlay(next: Overlay): void {
    popOverlayScope?.()
    popOverlayScope = shortcuts?.manager.pushScope(next)
    overlay = next
  }
  function closeOverlay(): void {
    overlay = undefined
    popOverlayScope?.()
    popOverlayScope = undefined
    void tick().then(focusGrid)
  }
  function focusGrid(): void {
    grid?.querySelector<HTMLElement>('[role="grid"]')?.focus({ preventScroll: true })
  }
  function toggleCharts(checked: boolean): void {
    if (compact.current) setChartDrawer(checked)
    else charts = checked
  }
  function setChartDrawer(open: boolean): void {
    popChartScope?.()
    popChartScope = open ? shortcuts?.manager.pushScope('charts') : undefined
    chartDrawer = open
  }
  $effect(() => {
    if (!compact.current && chartDrawer) setChartDrawer(false)
  })
  $effect(() => {
    if (!projection) return
    const current = createMoneyflowShortcuts(projection.capabilities ?? [], {
      local,
      apply: (action) => void apply(action),
    })
    shortcuts = current
    return () => current.destroy()
  })
  onMount(() => {
    if (!projection) return
    const keydown = (event: KeyboardEvent) =>
      shortcuts ? handleMoneyflowKeydown(shortcuts.manager, event) : false
    window.addEventListener('keydown', keydown)
    focusGrid()
    return () => {
      window.removeEventListener('keydown', keydown)
      popOverlayScope?.()
      popChartScope?.()
    }
  })
</script>

{#if projection}
  <div class="app-shell">
    <TopBar ariaLabel="Moneyflow">
      {#snippet left()}<span class="moneyflow-brand">Moneyflow</span>{/snippet}
      {#snippet right()}<Toggle
          checked={compact.current ? chartDrawer : charts}
          onchange={toggleCharts}
          label="Charts"
        /><ThemeToggle size="sm" />{/snippet}
    </TopBar>
    <RefinementBar {projection} onaction={(action) => void apply(action)} />
    <main class="app-shell__main" aria-label="Moneyflow workspace">
      <h1 class="kit-sr-only">Moneyflow transaction workspace</h1>
      <div class="app-shell__table" bind:this={grid}>
        <FinanceTable
          {projection}
          cursorIndex={controller.cursorIndex}
          onmove={(delta) => void controller.moveCursor(delta)}
          onhome={() => void controller.moveHome()}
          onactivate={(identity, kind) => void target('view.drill', identity, kind)}
          onselect={(identity, kind) => void target('selection.toggle', identity, kind)}
        />
      </div>
      {#if charts && !compact.current}
        <aside aria-label="Visualizations">
          <VisualizationRail
            {projection}
            cursorIndex={controller.cursorIndex}
            oncursor={(index) => void controller.moveCursorTo(index)}
            ondrill={(identity) => void target('view.drill', identity, 'aggregate')}
          />
        </aside>
      {/if}
    </main>
    <p class="kit-sr-only" aria-live="polite">{controller.announcement}</p>
    <p class="kit-sr-only" aria-live="polite">{controller.editing.state.announcement}</p>
    <StatusBar
      >{#snippet left()}<span
          >{projection.total_rows} results · <PendingStatus
            pending={controller.editing.state.pending}
          /></span
        >{/snippet}{#snippet right()}<span
          >profile revision {controller.editing.state.revision.toString(10)}</span
        >{/snippet}</StatusBar
    >
  </div>
  {#if overlay === 'search'}
    <SearchOverlay {controller} onclose={closeOverlay} />
  {:else if overlay === 'filters'}
    <FiltersDialog
      {projection}
      onapply={(action) => controller.apply(action)}
      onclose={closeOverlay}
    />
  {:else if overlay === 'help'}
    <HelpDialog capabilities={projection.capabilities ?? []} onclose={closeOverlay} />
  {:else if overlay === 'merchant' && focusedRow()}
    <MerchantDialog
      controller={controller.editing}
      target={focusedRow()!}
      hasSelection={projection.selection_count > 0}
      onclose={closeOverlay}
    />
  {:else if overlay === 'category' && focusedRow()}
    <CategoryDialog controller={controller.editing} target={focusedRow()!} onclose={closeOverlay} />
  {:else if overlay === 'categories'}
    <CategoryManager controller={controller.editing} onclose={closeOverlay} />
  {:else if overlay === 'groups'}
    <GroupManager controller={controller.editing} onclose={closeOverlay} />
  {:else if overlay === 'review'}
    <ReviewDrawer editing={controller.editing} review={controller.review} onclose={closeOverlay} />
  {/if}
  {#if compact.current && chartDrawer}
    <DetailDrawer
      title="Visualizations"
      ariaLabel="Moneyflow visualizations"
      onclose={() => setChartDrawer(false)}
    >
      <VisualizationRail
        {projection}
        cursorIndex={controller.cursorIndex}
        oncursor={(index) => void controller.moveCursorTo(index)}
        ondrill={(identity) => void target('view.drill', identity, 'aggregate')}
      />
    </DetailDrawer>
  {/if}
{/if}
