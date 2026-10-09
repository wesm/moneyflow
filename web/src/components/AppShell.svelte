<script lang="ts">
  import { CollapsibleSidebar, DetailDrawer, MEDIA, Toggle } from '@kenn-io/kit-ui'
  import { MediaQuery } from 'svelte/reactivity'
  import { onMount, untrack } from 'svelte'
  import FinanceTable from './FinanceTable.svelte'
  import ProviderConfirmation from './ProviderConfirmation.svelte'
  import RefinementBar from './RefinementBar.svelte'
  import ChartRail from './shell/ChartRail.svelte'
  import CommandBar from './shell/CommandBar.svelte'
  import EmptyScope from './shell/EmptyScope.svelte'
  import PeriodControl from './shell/PeriodControl.svelte'
  import ShellAnnouncements from './shell/ShellAnnouncements.svelte'
  import ShellOverlays from './shell/ShellOverlays.svelte'
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
  import { ChartState } from '../lib/shell/chart-state.svelte'
  import { keyHints } from '../lib/shell/key-hints'
  import {
    focusedRow,
    localActions,
    openTransactionInformation,
    type FocusedRow,
    type LocalActionContext,
  } from '../lib/shell/local-actions'
  import { SIDEBAR_FULL_QUERY } from '../lib/shell/layout'
  import type { NavRun } from '../lib/shell/nav-items'
  import { OverlayState } from '../lib/shell/overlay-state.svelte'
  import { pendingText } from '../lib/shell/pending'
  import { emptyScope } from '../lib/shell/period'
  import { connectionState, writeProgress } from '../lib/shell/status'
  interface Props {
    controller: ViewController
    onreconnect?: (() => void) | undefined
    onamazonimport?: (() => void) | undefined
  }
  let { controller, onreconnect, onamazonimport }: Props = $props()
  let grid = $state<HTMLElement | undefined>()
  let shortcuts: ReturnType<typeof createMoneyflowShortcuts> | undefined
  let transitionTail: Promise<void> = Promise.resolve()
  const projection = $derived(controller.projection)
  const emptyPeriod = $derived(projection && emptyScope(projection))
  const compact = new MediaQuery(MEDIA.compact)
  const fullSidebar = new MediaQuery(SIDEBAR_FULL_QUERY)
  const pushScope = (name: string) => shortcuts?.manager.pushScope(name)
  const overlays = new OverlayState({ pushScope, focusGrid, grid: () => grid })
  const charts = new ChartState({
    fullSidebar: () => fullSidebar.current,
    compact: () => compact.current,
    pushScope,
  })
  const write = $derived(writeProgress(controller.providerWrite.state.status))
  const actions: LocalActionContext = {
    get controller() {
      return controller
    },
    overlays,
    afterTransitions: () => transitionTail,
    importAmazon: () => onamazonimport?.(),
  }
  const localActionTable = localActions(actions)
  const local = (action: LocalAction) => localActionTable[action]()
  const row = () => focusedRow(projection, controller.cursorIndex)
  async function apply(action: string, dimension?: Dimension): Promise<void> {
    const focused = action === 'view.drill' || action === 'selection.toggle' ? row() : undefined
    if (focused) return target(action, focused.identity, focused.kind)
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
  async function target(action: string, identity: string, kind: FocusedRow['kind']): Promise<void> {
    await controller.apply({ action, target: { identity, kind } })
    focusGrid()
  }
  function focusGrid(): void {
    // A table request can finish after the user has opened a dialog.
    if (overlays.current !== undefined || charts.dialogOpen) return
    grid?.querySelector<HTMLElement>('[role="grid"]')?.focus({ preventScroll: true })
  }
  async function confirmDirectDelete(): Promise<void> {
    const deleteTarget = overlays.deleteTarget
    if (!deleteTarget) return
    const accepted = await controller.editing.submit({
      action: 'transaction.delete',
      input: {},
      target: deleteTarget,
    })
    if (accepted) overlays.close()
  }
  $effect(() => {
    if (!compact.current && charts.dialogOpen) charts.setDialog(false)
  })
  $effect(() => {
    if (charts.checked) untrack(() => charts.rail.load())
  })
  $effect(() => {
    if (!projection) return
    const current = createMoneyflowShortcuts(projection.capabilities ?? [], {
      local,
      apply: queueApply,
    })
    shortcuts = current
    overlays.rescope()
    charts.rescope()
    return () => current.destroy()
  })
  $effect(() => {
    if (controller.provider.state.phase !== 'confirmation') return
    const popScope = shortcuts?.manager.pushScope('provider-confirmation')
    return () => popScope?.()
  })
  $effect(() => {
    if (controller.providerWrite.state.phase === 'confirmation') overlays.open('write')
  })
  onMount(() => {
    if (!projection) return
    const keydown = (event: KeyboardEvent) =>
      shortcuts ? handleMoneyflowKeydown(shortcuts.manager, event) : false
    window.addEventListener('keydown', keydown)
    focusGrid()
    return () => {
      window.removeEventListener('keydown', keydown)
      overlays.destroy()
      charts.destroy()
    }
  })
</script>

{#if projection}
  {#snippet chartRail()}
    <ChartRail
      loader={charts.rail}
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
          onwrite={() => overlays.open('write')}
          {onreconnect}
        />
      {/snippet}
      <div class="app-shell__frame">
        {#if !fullSidebar.current && !compact.current}
          <SidebarRail
            {projection}
            pending={controller.editing.state.pending}
            provider={controller.provider}
            {write}
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
            {#snippet center()}
              <PeriodControl
                {projection}
                chooserOpen={overlays.current === 'period'}
                onchoose={() => overlays.open('period')}
                onclose={(focus) => overlays.close(focus)}
                onapply={(action) => void controller.apply(action)}
              />
            {/snippet}
            {#snippet trailing()}
              <Toggle
                checked={charts.checked}
                onchange={(checked) => charts.toggle(checked)}
                label="Charts"
              />
            {/snippet}
          </CommandBar>
          <main
            class="app-shell__main"
            class:app-shell__main--charts={charts.aside}
            aria-label="Moneyflow workspace"
          >
            <h1 class="kit-sr-only">Moneyflow transaction workspace</h1>
            <div class="app-shell__table" bind:this={grid}>
              {#if emptyPeriod}
                <EmptyScope
                  scope={emptyPeriod}
                  onjump={() =>
                    void controller
                      .apply({ action: 'time.set-period', period: emptyPeriod.jump })
                      .then(focusGrid)}
                />
              {:else}
                <FinanceTable
                  {projection}
                  cursorIndex={controller.cursorIndex}
                  onmove={(delta) => void controller.moveCursor(delta)}
                  onhome={() => void controller.moveHome()}
                  onactivate={(identity, kind) => void target('view.drill', identity, kind)}
                  onselect={(identity, kind) => void target('selection.toggle', identity, kind)}
                  oninformation={(identity) => void openTransactionInformation(actions, identity)}
                />
              {/if}
            </div>
            {#if charts.aside}
              <aside aria-label="Visualizations">
                {@render chartRail()}
              </aside>
            {/if}
          </main>
          <ShellAnnouncements {controller} chartMessage={charts.rail.message} />
          <StatusLine
            hints={keyHints(projection, overlays.current !== undefined || charts.dialogOpen)}
            connection={connectionState(controller.problem, controller.loading)}
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
    {overlays}
    row={row()}
    onconfirmdelete={() => void confirmDirectDelete()}
    {onreconnect}
  />
  <ProviderConfirmation controller={controller.provider} />
  {#if compact.current && charts.dialogOpen}
    <DetailDrawer
      title="Visualizations"
      ariaLabel="Moneyflow visualizations"
      width="min(560px, calc(100vw - 1px))"
      onclose={() => charts.setDialog(false)}
    >
      {@render chartRail()}
    </DetailDrawer>
  {/if}
{/if}
