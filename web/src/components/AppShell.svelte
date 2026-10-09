<script lang="ts">
  import { CollapsibleSidebar, DetailDrawer, MEDIA, Toggle } from '@kenn-io/kit-ui'
  import { MediaQuery } from 'svelte/reactivity'
  import { onMount, tick, untrack } from 'svelte'
  import FinanceTable from './FinanceTable.svelte'
  import ProviderConfirmation from './ProviderConfirmation.svelte'
  import RefinementBar from './RefinementBar.svelte'
  import ChartRail from './shell/ChartRail.svelte'
  import CommandBar from './shell/CommandBar.svelte'
  import ShellAnnouncements from './shell/ShellAnnouncements.svelte'
  import ShellOverlays, { type Overlay } from './shell/ShellOverlays.svelte'
  import Sidebar from './shell/Sidebar.svelte'
  import SidebarRail from './shell/SidebarRail.svelte'
  import StatusLine from './shell/StatusLine.svelte'
  import {
    createMoneyflowShortcuts,
    handleMoneyflowKeydown,
    type Dimension,
    type LocalAction,
  } from '../lib/shortcuts'
  import type { ViewController } from '../lib/controller/view-controller.svelte'
  import type { TransactionInformationResponse } from '../lib/api/client'
  import { keyHints } from '../lib/shell/key-hints'
  import { SIDEBAR_FULL_QUERY } from '../lib/shell/layout'
  import type { NavRun } from '../lib/shell/nav-items'
  import { pendingText } from '../lib/shell/pending'
  import { RailLoader } from '../lib/shell/rail-loader.svelte'
  interface Props {
    controller: ViewController
    onreconnect?: (() => void) | undefined
    onamazonimport?: (() => void) | undefined
  }
  let { controller, onreconnect, onamazonimport }: Props = $props()
  let charts = $state(true)
  const rail = new RailLoader()
  let chartDrawer = $state(false)
  let grid = $state<HTMLElement | undefined>()
  let overlay = $state<Overlay | undefined>()
  let popOverlayScope: (() => void) | undefined
  let popChartScope: (() => void) | undefined
  let shortcuts: ReturnType<typeof createMoneyflowShortcuts> | undefined
  let focusRestoreFrame: number | undefined
  let deleteTarget = $state<{ identity: string; kind: 'transaction' } | undefined>()
  let deleteCount = $state(0)
  let transactionInformation = $state<TransactionInformationResponse | undefined>()
  let transitionTail: Promise<void> = Promise.resolve()
  const projection = $derived(controller.projection)
  const compact = new MediaQuery(MEDIA.compact)
  const fullSidebar = new MediaQuery(SIDEBAR_FULL_QUERY)
  const writeStatus = $derived(controller.providerWrite.state.status)
  const write = $derived(
    writeStatus?.phase ? { completed: writeStatus.completed, total: writeStatus.total } : undefined,
  )
  const connection = $derived(
    controller.problem?.code === 'network_error'
      ? 'offline'
      : controller.loading
        ? 'updating'
        : 'connected',
  )

  function local(action: LocalAction): void {
    if (action === 'cursor.up') void controller.moveCursor(-1)
    else if (action === 'cursor.down') void controller.moveCursor(1)
    else if (action === 'cursor.home') void controller.moveHome()
    else if (action === 'overlay.search') openOverlay('search')
    else if (action === 'overlay.filters') openOverlay('filters')
    else if (action === 'overlay.help') openOverlay('help')
    else if (action === 'edit.merchant') {
      if (focusedRow()) openOverlay('merchant')
    } else if (action === 'edit.category') {
      if (focusedRow()) openOverlay('category')
    } else if (action === 'manage.categories') openOverlay('categories')
    else if (action === 'manage.groups') openOverlay('groups')
    else if (action === 'edit.review')
      openOverlay(controller.providerWrite.state.status?.phase ? 'write' : 'review')
    else if (action === 'view.duplicates') openOverlay('duplicates')
    else if (action === 'provider.refresh') {
      if (projection?.profile_kind === 'amazon') onamazonimport?.()
      else void controller.provider.refresh()
    } else if (action === 'transactions.export') void beginExport()
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
    } else if (action === 'edit.delete') {
      const row = focusedRow()
      if (row?.kind === 'transaction') {
        deleteTarget = { identity: row.identity, kind: 'transaction' }
        deleteCount = projection?.selection_count ? projection.selection_count : 1
        openOverlay('delete')
      }
    } else if (action === 'transaction.info') {
      void transitionTail.then(async () => {
        await tick()
        const row = focusedRow()
        if (row?.kind === 'transaction') await openTransactionInformation(row.identity)
      })
    }
  }
  async function beginExport(): Promise<void> {
    if (!projection) return
    if (await controller.export.open(projection.canonical_query)) openOverlay('export')
  }
  async function openTransactionInformation(identity: string): Promise<void> {
    const information = await controller.transactionInformation?.(identity)
    if (!information) return
    transactionInformation = information
    openOverlay('information')
  }
  function focusedRow(): { identity: string; kind: 'transaction' | 'aggregate' } | undefined {
    const row = [...(projection?.detail_rows ?? []), ...(projection?.aggregate_rows ?? [])].find(
      (candidate) => candidate.index === controller.cursorIndex,
    )
    return row
      ? { identity: row.identity, kind: projection?.detail_rows ? 'transaction' : 'aggregate' }
      : undefined
  }
  async function apply(action: string, dimension?: Dimension): Promise<void> {
    if (action === 'view.drill' || action === 'selection.toggle') {
      const row = [...(projection?.detail_rows ?? []), ...(projection?.aggregate_rows ?? [])].find(
        (candidate) => candidate.index === controller.cursorIndex,
      )
      if (row) {
        await target(action, row.identity, projection?.detail_rows ? 'transaction' : 'aggregate')
        return
      }
    }
    await controller.apply({ action, ...(dimension === undefined ? {} : { dimension }) })
    focusGrid()
  }
  function run(entry: NavRun): void {
    if (entry.kind === 'local') local(entry.action)
    else
      queueApply(entry.action, entry.action === 'view.set-grouping' ? entry.dimension : undefined)
  }
  function queueApply(action: string, dimension?: Dimension): void {
    const run = () => apply(action, dimension)
    transitionTail = transitionTail.then(run, run)
  }
  async function target(
    action: string,
    identity: string,
    kind: 'transaction' | 'aggregate',
  ): Promise<void> {
    await controller.apply({ action, target: { identity, kind } })
    focusGrid()
  }
  function openOverlay(next: Overlay): void {
    if (focusRestoreFrame !== undefined) cancelAnimationFrame(focusRestoreFrame)
    focusRestoreFrame = undefined
    popOverlayScope?.()
    popOverlayScope = shortcuts?.manager.pushScope(next)
    overlay = next
  }
  function closeOverlay(): void {
    overlay = undefined
    popOverlayScope?.()
    popOverlayScope = undefined
    deleteTarget = undefined
    deleteCount = 0
    transactionInformation = undefined
    void tick().then(() => {
      focusRestoreFrame = requestAnimationFrame(() => {
        focusRestoreFrame = undefined
        if (overlay === undefined) focusGrid()
      })
    })
  }
  function focusGrid(): void {
    // A table request can finish after the user has opened a dialog.
    if (overlay !== undefined || chartDrawer) return
    grid?.querySelector<HTMLElement>('[role="grid"]')?.focus({ preventScroll: true })
  }
  async function confirmDirectDelete(): Promise<void> {
    if (!deleteTarget) return
    const accepted = await controller.editing.submit({
      action: 'transaction.delete',
      input: {},
      target: deleteTarget,
    })
    if (accepted) closeOverlay()
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
  const chartsShown = $derived(compact.current ? chartDrawer : charts)
  $effect(() => {
    if (chartsShown) untrack(() => rail.load())
  })
  $effect(() => {
    if (!projection) return
    const current = createMoneyflowShortcuts(projection.capabilities ?? [], {
      local,
      apply: queueApply,
    })
    shortcuts = current
    const activeOverlay = untrack(() => overlay)
    const activeChartDrawer = untrack(() => chartDrawer)
    popOverlayScope = activeOverlay ? current.manager.pushScope(activeOverlay) : undefined
    popChartScope = activeChartDrawer ? current.manager.pushScope('charts') : undefined
    return () => current.destroy()
  })
  $effect(() => {
    if (controller.provider.state.phase !== 'confirmation') return
    const popScope = shortcuts?.manager.pushScope('provider-confirmation')
    return () => popScope?.()
  })
  $effect(() => {
    if (controller.providerWrite.state.phase === 'confirmation') openOverlay('write')
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
      if (focusRestoreFrame !== undefined) cancelAnimationFrame(focusRestoreFrame)
    }
  })
</script>

{#if projection}
  {#snippet chartRail()}
    <ChartRail
      loader={rail}
      {projection}
      cursorIndex={controller.cursorIndex}
      oncursor={(index) => void controller.moveCursorTo(index)}
      ondrill={(identity) => void target('view.drill', identity, 'aggregate')}
    />
  {/snippet}
  <div class="app-shell">
    <CollapsibleSidebar
      hideSidebar={!fullSidebar.current}
      sidebarWidth={176}
      minSidebarWidth={176}
      maxSidebarWidth={280}
      mainOverflow="hidden"
    >
      {#snippet sidebar()}
        <Sidebar
          {projection}
          pending={controller.editing.state.pending}
          {write}
          provider={controller.provider}
          onrun={run}
          onreview={() => local('edit.review')}
          onwrite={() => openOverlay('write')}
          {onreconnect}
        />
      {/snippet}
      <div class="app-shell__frame">
        {#if !fullSidebar.current && !compact.current}
          <SidebarRail
            {projection}
            pending={controller.editing.state.pending}
            provider={controller.provider}
            writing={write !== undefined}
            onrun={run}
            onreview={() => local('edit.review')}
            {onreconnect}
          />
        {/if}
        <div class="app-shell__column">
          <CommandBar>
            {#snippet leading()}
              <RefinementBar {projection} onaction={(action) => void apply(action)} />
            {/snippet}
            {#snippet trailing()}
              <Toggle
                checked={compact.current ? chartDrawer : charts}
                onchange={toggleCharts}
                label="Charts"
              />
            {/snippet}
          </CommandBar>
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
                oninformation={(identity) => void openTransactionInformation(identity)}
              />
            </div>
            {#if charts && !compact.current}
              <aside aria-label="Visualizations">
                {@render chartRail()}
              </aside>
            {/if}
          </main>
          <ShellAnnouncements {controller} chartMessage={rail.message} />
          <StatusLine
            hints={keyHints(projection, overlay !== undefined || chartDrawer)}
            {connection}
            revision={controller.editing.state.revision.toString(10)}
            pending={fullSidebar.current
              ? undefined
              : pendingText(controller.editing.state.pending)}
            compact={compact.current}
            onretry={() => void controller.retry()}
          />
        </div>
      </div>
    </CollapsibleSidebar>
  </div>
  <ShellOverlays
    {controller}
    {projection}
    {overlay}
    row={focusedRow()}
    {deleteCount}
    deleting={deleteTarget !== undefined}
    information={transactionInformation}
    onclose={closeOverlay}
    onwrite={() => openOverlay('write')}
    onconfirmdelete={() => void confirmDirectDelete()}
    {onreconnect}
  />
  <ProviderConfirmation controller={controller.provider} />
  {#if compact.current && chartDrawer}
    <DetailDrawer
      title="Visualizations"
      ariaLabel="Moneyflow visualizations"
      width="min(560px, calc(100vw - 1px))"
      onclose={() => setChartDrawer(false)}
    >
      {@render chartRail()}
    </DetailDrawer>
  {/if}
{/if}
