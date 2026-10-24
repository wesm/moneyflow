<script lang="ts">
  import { FlashBanner, MEDIA } from '@kenn-io/kit-ui'
  import { MediaQuery } from 'svelte/reactivity'
  import { onMount, tick, untrack } from 'svelte'
  import ProviderStatus from './ProviderStatus.svelte'
  import RefreshDialog from './RefreshDialog.svelte'
  import CommandBar from './shell/CommandBar.svelte'
  import LoadingSidebar from './shell/LoadingSidebar.svelte'
  import PendingCard from './shell/PendingCard.svelte'
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
  import { writeNeedsUser, writeSaved } from '../lib/controller/provider-write'
  import type { TransitionAction, ViewController } from '../lib/controller/view-controller.svelte'
  import { providerName } from '../lib/provider-name'
  import { availableCapabilities } from '../lib/shell/capabilities'
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
  import { hasProviderStatus, navItems, type NavRun } from '../lib/shell/nav-items'
  import { OverlayState } from '../lib/shell/overlay-state.svelte'
  import { pendingText } from '../lib/shell/pending'
  import { connectionState, status, writeProgress } from '../lib/shell/status.svelte'
  import { TransitionQueue } from '../lib/shell/transition-queue'
  import type { ViewID } from '../lib/views/views'
  interface Props {
    controller: ViewController
    onreconnect?: (() => void) | undefined
    onamazonimport?: (() => void) | undefined
    /** Renders moneyflow's sidebar and page landmarks. Without it, the host page has them. */
    chrome?: boolean
    /** Handles moneyflow's keys anywhere on the page. Without it, only inside `.mf-views`. */
    globalKeys?: boolean
    /** Reports the active explore view after each change. */
    onlocation?: ((view: ViewID | undefined) => void) | undefined
  }
  let {
    controller,
    onreconnect,
    onamazonimport,
    chrome = true,
    globalKeys = true,
    onlocation,
  }: Props = $props()
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
  const lastWrite = $derived(writeSaved(controller.providerWrite.state))
  /** Without the sidebar, the pending card shows only while it has something to report. */
  const pendingCard = $derived.by(() => {
    const pending = controller.editing.state.pending
    return pending.active_operations > 0 || pending.inactive_operations > 0 || !!write || lastWrite
  })
  const entries = $derived(projection ? navItems(projection) : undefined)
  const location = $derived(
    entries?.explore.find((entry) => entry.active)?.id as ViewID | undefined,
  )
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
    queue: (work, dropped) => transitions.run(work, dropped),
    get stopped() {
      return transitions.stopped
    },
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
  /** Shows a view as its sidebar entry does. An unavailable view says why in the status line. */
  export function open(view: ViewID): void {
    const entry = [...(entries?.explore ?? []), ...(entries?.maintain ?? [])].find(
      (item) => item.id === view,
    )
    if (!entry) return
    if (!entry.available) {
      if (entry.reason) status.notify(entry.reason)
      return
    }
    run(entry.run)
  }
  /** Opens the review of pending edits as the Review button does. */
  export function review(): void {
    local('edit.review')
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
  $effect(() => onlocation?.(location))
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
  /**
   * The grid takes focus once, when the first view arrives. In a host page it takes focus only
   * from the page body, so a host control the user moved to while the view loaded keeps it.
   */
  const loaded = $derived(projection !== undefined)
  $effect(() => {
    if (!loaded) return
    untrack(() => {
      if (chrome || document.activeElement === document.body) focusGrid()
    })
  })
  onMount(() => {
    const keydown = (event: KeyboardEvent) => {
      const inViews = event.target instanceof Element && event.target.closest('.mf-views')
      if (!globalKeys && !inViews) return false
      return shortcuts && !controller.provider.state.dialog
        ? handleMoneyflowKeydown(shortcuts.manager, event)
        : false
    }
    window.addEventListener('keydown', keydown)
    return () => {
      window.removeEventListener('keydown', keydown)
      transitions.stop()
      overlays.destroy()
      charts.destroy()
    }
  })
</script>

<div class="app-shell" inert={refreshing}>
  {#if chrome && projection}
    <Sidebar
      {projection}
      pending={controller.editing.state.pending}
      {write}
      {lastWrite}
      provider={controller.provider}
      onrun={(entry) => fromSidebar(() => run(entry))}
      onreview={() => fromSidebar(() => local('edit.review'))}
      onwrite={() => fromSidebar(() => overlays.open('write'))}
      {onreconnect}
      onfooter={(footer) => (sidebarFooter = footer)}
    />
  {:else if chrome}
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
        banner={chrome}
        onfocusgrid={focusGrid}
      />
    {:else}
      <CommandBar banner={chrome}>{#snippet leading()}{/snippet}</CommandBar>
    {/if}
    <svelte:element
      this={chrome ? 'main' : 'section'}
      class="app-shell__main"
      aria-label="Moneyflow workspace"
      aria-busy={projection ? undefined : 'true'}
      use:trackTop={(top: number) => (workspaceTop = top)}
    >
      <svelte:element this={chrome ? 'h1' : 'h2'} class="kit-sr-only">
        Moneyflow transaction workspace
      </svelte:element>
      <div class="app-shell__table" bind:this={grid}>
        {#if projection}
          <WorkspaceBody {projection} {actions} onapply={queueSend} {charts} {onreconnect} />
        {/if}
      </div>
    </svelte:element>
    <ShellAnnouncements {controller} />
    {#if !chrome && projection && (pendingCard || hasProviderStatus(projection.profile_kind))}
      <div class="mf-workspace-status">
        {#if pendingCard}
          <PendingCard
            pending={controller.editing.state.pending}
            reviewAvailable={availableCapabilities(projection).has('changes.review')}
            {write}
            {lastWrite}
            onreview={() => local('edit.review')}
            onwrite={() => overlays.open('write')}
          />
        {/if}
        {#if hasProviderStatus(projection.profile_kind)}
          <ProviderStatus controller={controller.provider} {onreconnect} />
        {/if}
      </div>
    {/if}
    <StatusLine
      hints={projection
        ? keyHints(projection, overlays.current !== undefined || charts.dialogOpen)
        : []}
      connection={connectionState(controller.problem, controller.loading)}
      revision={chrome && projection ? controller.editing.state.revision.toString(10) : undefined}
      {activity}
      pending={!projection || sidebarFooter || !chrome
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
