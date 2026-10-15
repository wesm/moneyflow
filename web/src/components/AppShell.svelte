<script lang="ts">
  import { FlashBanner, MEDIA } from '@kenn-io/kit-ui'
  import { MediaQuery } from 'svelte/reactivity'
  import { onMount, tick, untrack } from 'svelte'
  import RefreshDialog from './RefreshDialog.svelte'
  import CommandBar from './shell/CommandBar.svelte'
  import LoadingSidebar from './shell/LoadingSidebar.svelte'
  import ShellAnnouncements from './shell/ShellAnnouncements.svelte'
  import ShellCommandBar from './shell/ShellCommandBar.svelte'
  import ShellOverlays from './shell/ShellOverlays.svelte'
  import Sidebar from './shell/Sidebar.svelte'
  import StatusLine from './shell/StatusLine.svelte'
  import WorkspaceBody from './shell/WorkspaceBody.svelte'
  import {
    createMoneyflowShortcuts,
    handleMoneyflowKeydown,
    type Dimension,
    type LocalAction,
  } from '../lib/shortcuts'
  import { writeNeedsUser } from '../lib/controller/provider-write'
  import type { TransitionAction, ViewController } from '../lib/controller/view-controller.svelte'
  import { providerName } from '../lib/provider-name'
  import { ChartState } from '../lib/shell/chart-state.svelte'
  import { trackTop } from '../lib/shell/flash-top'
  import { keyHints } from '../lib/shell/key-hints'
  import {
    confirmDirectDelete,
    focusedRow,
    localActions,
    openTransactionInformation,
    type LocalActionContext,
  } from '../lib/shell/local-actions'
  import { inSearch } from '../lib/shell/layout'
  import type { NavRun } from '../lib/shell/nav-items'
  import { OverlayState } from '../lib/shell/overlay-state.svelte'
  import { pendingText } from '../lib/shell/pending'
  import { connectionState, status, writeProgress } from '../lib/shell/status.svelte'
  import { TransitionQueue } from '../lib/shell/transition-queue'
  interface Props {
    controller: ViewController
    onreconnect?: (() => void) | undefined
    onamazonimport?: (() => void) | undefined
  }
  let { controller, onreconnect, onamazonimport }: Props = $props()
  let grid = $state<HTMLElement | undefined>()
  /** Where the workspace starts below the command bar; failure banners sit there. */
  let workspaceTop = $state(0)
  let commandBar = $state<ShellCommandBar>()
  /** The sidebar's full footer while it shows; inside a `<dialog>`, the narrow drawer is open. */
  let sidebarFooter = $state<HTMLElement | undefined>()
  const drawer = $derived(sidebarFooter?.closest('dialog') ?? undefined)
  let shortcuts: ReturnType<typeof createMoneyflowShortcuts> | undefined
  const transitions = new TransitionQueue(() => controller)
  const projection = $derived(controller.projection)
  const compact = new MediaQuery(MEDIA.compact)
  const pushScope = (name: string) => shortcuts?.manager.pushScope(name)
  const overlays = new OverlayState({
    pushScope,
    focusGrid,
    grid: () => grid,
    // Any other overlay, even one that opens by itself, such as a write confirmation, closes the
    // drawer first. It then renders outside the modal drawer and takes focus.
    opening: (next) => {
      if (next !== 'navigation') drawer?.close()
    },
  })
  const charts = new ChartState({ compact: () => compact.current, pushScope })
  const write = $derived(writeProgress(controller.providerWrite.state.status))
  /** A running write's progress, which the status bar shows in place of the key hints. */
  const writing = $derived(
    controller.providerWrite.state.phase === 'active' && write && projection
      ? `Writing to ${providerName(projection.profile_kind)}: ${write.completed} of ${write.total}…`
      : undefined,
  )
  const activity = $derived(projection ? writing : 'Loading…')
  const actions: LocalActionContext = {
    get controller() {
      return controller
    },
    overlays,
    queue: (work) => transitions.run(work),
    focusGrid,
    importAmazon: () => onamazonimport?.(),
    focusSearch: () => void commandBar?.focusSearch(),
    openFilters: () => void commandBar?.openFilters(),
  }
  const local = (action: LocalAction) => localActions(actions)[action]()
  async function apply(action: string, dimension?: Dimension) {
    const row = focusedRow(controller.projection, controller.cursorIndex)
    const focused = action === 'view.drill' || action === 'selection.toggle' ? row : undefined
    if (action === 'view.drill' && focused?.kind === 'transaction')
      return openTransactionInformation(actions, focused.identity)
    if (focused) return send({ action, target: focused })
    await send({ action, ...(dimension === undefined ? {} : { dimension }) })
  }
  function run(entry: NavRun): void {
    if (entry.kind === 'local') local(entry.action)
    else
      queueApply(entry.action, entry.action === 'view.set-grouping' ? entry.dimension : undefined)
  }
  /**
   * Runs a sidebar action. From the drawer it waits until the drawer has closed, so an overlay it
   * opens owns focus.
   */
  function fromSidebar(work: () => void): void {
    if (!drawer) return work()
    drawer.close()
    overlays.close('keep')
    void tick().then(work)
  }
  /** Queuing a change is the user's next action, so it clears the notice now, not when it runs. */
  function queue(work: () => unknown): Promise<unknown> {
    status.clear()
    return actions.queue(work)
  }
  /** Space toggles the cursor row when its turn comes; a later cursor move queues behind it. */
  const queueApply = (action: string, dimension?: Dimension) =>
    void queue(() => apply(action, dimension))
  const queueSend = (request: TransitionAction) => queue(() => send(request))
  async function send(request: TransitionAction): Promise<void> {
    await controller.apply(request)
    focusGrid()
  }
  function focusGrid(fromSearch = false): void {
    // A request can finish after the user has opened a dialog or moved into search.
    if (overlays.current !== undefined || charts.dialogOpen || (!fromSearch && inSearch())) return
    const table = grid?.querySelector<HTMLElement>('[role="grid"], .mf-table-empty')
    table?.focus({ preventScroll: true })
  }
  $effect(() => {
    if (compact.current) return
    if (charts.dialogOpen) charts.setDialog(false)
    if (overlays.current === 'view') overlays.close()
  })
  // The open drawer is the navigation overlay: it holds the shortcut scope, so keys pressed in it
  // do not reach the grid behind it.
  $effect(() => {
    if (!drawer) return
    overlays.open('navigation')
    return () => {
      if (untrack(() => overlays.current) === 'navigation') overlays.close('keep')
      if (document.activeElement === document.body) focusGrid()
    }
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
  /** The refresh dialog is modal: the workspace behind it is inert and takes no keys. */
  const refreshing = $derived(controller.provider.state.dialog !== undefined)
  $effect(() => {
    if (!refreshing) return
    // A refresh pressed in the drawer opens the dialog; the drawer gives way to it.
    untrack(() => drawer)?.close()
    return () => void tick().then(() => focusGrid())
  })
  /** A routine write shows in the status bar; one that needs the user opens its drawer. */
  const writeBlocked = $derived(writeNeedsUser(controller.providerWrite.state))
  $effect(() => {
    if (writeBlocked) overlays.open('write')
  })
  /** A write that starts replaces an older notice, so its progress shows. */
  const writeStarted = $derived(writing !== undefined)
  $effect(() => {
    if (writeStarted) status.clear()
  })
  /** The grid takes focus once, when the first view arrives. */
  const loaded = $derived(projection !== undefined)
  $effect(() => {
    if (loaded) untrack(() => focusGrid())
  })
  onMount(() => {
    const keydown = (event: KeyboardEvent) =>
      shortcuts && !controller.provider.state.dialog
        ? handleMoneyflowKeydown(shortcuts.manager, event)
        : false
    window.addEventListener('keydown', keydown)
    return () => {
      window.removeEventListener('keydown', keydown)
      overlays.destroy()
      charts.destroy()
    }
  })
</script>

<div class="app-shell" inert={refreshing}>
  {#if projection}
    <Sidebar
      {projection}
      pending={controller.editing.state.pending}
      {write}
      provider={controller.provider}
      onrun={(entry) => fromSidebar(() => run(entry))}
      onreview={() => fromSidebar(() => local('edit.review'))}
      onwrite={() => fromSidebar(() => overlays.open('write'))}
      {onreconnect}
      onfooter={(footer) => (sidebarFooter = footer)}
    />
  {:else}
    <LoadingSidebar />
  {/if}
  <div class="app-shell__column">
    {#if projection}
      <ShellCommandBar
        bind:this={commandBar}
        {controller}
        {projection}
        {overlays}
        {charts}
        compact={compact.current}
        queue={transitions}
        onfocusgrid={focusGrid}
      />
    {:else}
      <CommandBar>{#snippet leading()}{/snippet}</CommandBar>
    {/if}
    <main
      class="app-shell__main"
      aria-label="Moneyflow workspace"
      aria-busy={projection ? undefined : 'true'}
      use:trackTop={(top) => (workspaceTop = top)}
    >
      <h1 class="kit-sr-only">Moneyflow transaction workspace</h1>
      <div class="app-shell__table" bind:this={grid}>
        {#if projection}
          <WorkspaceBody {projection} {actions} onapply={queueSend} {charts} {onreconnect} />
        {/if}
      </div>
    </main>
    <ShellAnnouncements {controller} />
    <StatusLine
      hints={projection
        ? keyHints(projection, overlays.current !== undefined || charts.dialogOpen)
        : []}
      connection={connectionState(controller.problem, controller.loading)}
      revision={projection ? controller.editing.state.revision.toString(10) : undefined}
      {activity}
      pending={!projection || sidebarFooter
        ? undefined
        : pendingText(controller.editing.state.pending)}
      compact={compact.current}
      onretry={() => void controller.retry()}
    />
  </div>
</div>
{#if projection}
  <ShellOverlays
    {controller}
    {projection}
    {overlays}
    onconfirmdelete={() => confirmDirectDelete(actions)}
    {onreconnect}
  />
  <!-- After the drawer closes, so the drawer's focus return cannot pull focus out of it. -->
  {#if !drawer}
    <RefreshDialog controller={controller.provider} {onreconnect} />
  {/if}
{/if}
<FlashBanner top="calc({workspaceTop}px + var(--space-4))" />
