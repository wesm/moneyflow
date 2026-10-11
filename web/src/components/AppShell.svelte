<script lang="ts">
  import { CollapsibleSidebar, MEDIA } from '@kenn-io/kit-ui'
  import { MediaQuery } from 'svelte/reactivity'
  import { onMount, tick, untrack } from 'svelte'
  import ProviderConfirmation from './ProviderConfirmation.svelte'
  import ChartDrawer from './shell/ChartDrawer.svelte'
  import ChartRail from './shell/ChartRail.svelte'
  import NavDrawer from './shell/NavDrawer.svelte'
  import ShellAnnouncements from './shell/ShellAnnouncements.svelte'
  import ShellCommandBar from './shell/ShellCommandBar.svelte'
  import ShellOverlays from './shell/ShellOverlays.svelte'
  import Sidebar from './shell/Sidebar.svelte'
  import SidebarRail from './shell/SidebarRail.svelte'
  import StatusLine from './shell/StatusLine.svelte'
  import WorkspaceBody from './shell/WorkspaceBody.svelte'
  import {
    createMoneyflowShortcuts,
    handleMoneyflowKeydown,
    type Dimension,
    type LocalAction,
  } from '../lib/shortcuts'
  import type { TransitionAction, ViewController } from '../lib/controller/view-controller.svelte'
  import { ChartState } from '../lib/shell/chart-state.svelte'
  import { keyHints } from '../lib/shell/key-hints'
  import {
    confirmDirectDelete,
    localActions,
    moveCursor,
    openTransactionInformation,
    pressedRow,
    type LocalActionContext,
  } from '../lib/shell/local-actions'
  import { SIDEBAR_FULL_QUERY, inSearch } from '../lib/shell/layout'
  import type { NavRun } from '../lib/shell/nav-items'
  import { OverlayState } from '../lib/shell/overlay-state.svelte'
  import { pendingText } from '../lib/shell/pending'
  import { connectionState, status, writeProgress } from '../lib/shell/status.svelte'
  interface Props {
    controller: ViewController
    onreconnect?: (() => void) | undefined
    onamazonimport?: (() => void) | undefined
  }
  let { controller, onreconnect, onamazonimport }: Props = $props()
  let grid = $state<HTMLElement | undefined>()
  let commandBar = $state<ShellCommandBar>()
  let shortcuts: ReturnType<typeof createMoneyflowShortcuts> | undefined
  let transitionTail: Promise<unknown> = Promise.resolve()
  const projection = $derived(controller.projection)
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
    queue,
    focusGrid,
    importAmazon: () => onamazonimport?.(),
    focusSearch: () => void commandBar?.focusSearch(),
    openFilters: () => void commandBar?.openFilters(),
  }
  const local = (action: LocalAction) => localActions(actions)[action]()
  async function apply(action: string, dimension?: Dimension, row = pressedRow(controller)) {
    const focused = action === 'view.drill' || action === 'selection.toggle' ? row() : undefined
    if (action === 'view.drill' && focused?.kind === 'transaction')
      return void openTransactionInformation(actions, focused.identity)
    if (focused) return send({ action, target: focused })
    await send({ action, ...(dimension === undefined ? {} : { dimension }) })
  }
  function run(entry: NavRun): void {
    if (entry.kind === 'local') local(entry.action)
    else
      queueApply(entry.action, entry.action === 'view.set-grouping' ? entry.dimension : undefined)
  }
  /** Runs a drawer entry once the drawer has closed, so an overlay it opens owns focus. */
  function fromDrawer(work: () => void): void {
    overlays.close('keep')
    void tick().then(work)
  }
  /**
   * Runs view transitions one at a time, so each starts from the result of the last. Queuing
   * one is the user's next action, so it clears the notice now, not when the transition runs.
   */
  function queue(work: () => unknown): Promise<unknown> {
    status.clear()
    return (transitionTail = transitionTail.then(work, work))
  }
  /** Space toggles the row it was pressed on, unless a view change queued before it runs. */
  function queueApply(action: string, dimension?: Dimension): void {
    const pressed = action === 'selection.toggle' ? pressedRow(controller) : undefined
    queue(() => apply(action, dimension, pressed))
  }
  const queueSend = (request: TransitionAction) => queue(() => send(request))
  async function send(request: TransitionAction): Promise<void> {
    await controller.apply(request)
    focusGrid()
  }
  function focusGrid(fromSearch = false): void {
    // A request can finish after the user has opened a dialog or moved into search.
    if (overlays.current !== undefined || charts.dialogOpen || (!fromSearch && inSearch())) return
    grid?.querySelector<HTMLElement>('[role="grid"]')?.focus({ preventScroll: true })
  }
  $effect(() => {
    if (compact.current) return
    if (charts.dialogOpen) charts.setDialog(false)
    const open = overlays.current
    if (open === 'navigation' || open === 'view') overlays.close()
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
    // A refresh pressed in the phone drawer asks for confirmation; the drawer gives way to it.
    if (untrack(() => overlays.current) === 'navigation') overlays.close('keep')
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
  {#snippet navigation(then: (work: () => void) => void)}
    <Sidebar
      {projection}
      pending={controller.editing.state.pending}
      {write}
      provider={controller.provider}
      onrun={(entry) => then(() => run(entry))}
      onreview={() => then(() => local('edit.review'))}
      onwrite={() => then(() => overlays.open('write'))}
      {onreconnect}
    />
  {/snippet}
  {#snippet chartRail()}
    <ChartRail
      loader={charts.rail}
      {projection}
      cursorIndex={controller.cursorIndex}
      oncursor={(index) => moveCursor(() => controller.moveCursorTo(index))}
      ondrill={(identity) =>
        queueSend({ action: 'view.drill', target: { identity, kind: 'aggregate' } })}
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
        {@render navigation((work) => work())}
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
          <ShellCommandBar
            bind:this={commandBar}
            {controller}
            {projection}
            {overlays}
            {charts}
            compact={compact.current}
            onfocusgrid={() => focusGrid(true)}
          />
          <main
            class="app-shell__main"
            class:app-shell__main--charts={charts.aside}
            aria-label="Moneyflow workspace"
          >
            <h1 class="kit-sr-only">Moneyflow transaction workspace</h1>
            <div class="app-shell__table" bind:this={grid}>
              <WorkspaceBody {projection} {actions} onapply={queueSend} />
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
            pending={fullSidebar.current || overlays.current === 'navigation'
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
    onconfirmdelete={() => void confirmDirectDelete(actions)}
    {onreconnect}
  />
  <NavDrawer
    open={compact.current && overlays.current === 'navigation'}
    onclose={() => overlays.close()}
  >
    {@render navigation(fromDrawer)}
  </NavDrawer>
  <!-- After the drawer closes, so the drawer's focus return cannot pull focus out of it. -->
  {#if overlays.current !== 'navigation'}
    <ProviderConfirmation controller={controller.provider} />
  {/if}
  <ChartDrawer open={compact.current && charts.dialogOpen} onclose={() => charts.setDialog(false)}>
    {@render chartRail()}
  </ChartDrawer>
{/if}
