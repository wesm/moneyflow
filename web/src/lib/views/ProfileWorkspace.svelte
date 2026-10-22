<script lang="ts" module>
  /** Whether a key has been pressed on this page. The grid's focus ring shows from then on. */
  let keyboardUsed = false
</script>

<script lang="ts">
  import { Button, StatusBar, ThemeToggle, TopBar } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'

  import AppShell from '../../components/AppShell.svelte'
  import { createMoneyflowClient } from '../api/client'
  import { createViewController, type ViewController } from '../controller/view-controller.svelte'
  import type { ViewStateAdapter } from '../controller/view-state'
  import { navItems } from '../shell/nav-items'
  import type { ViewID } from './views'

  interface Props {
    /**
     * Where the host mounts moneyflow's API, such as '/embedded/moneyflow/'. Read when the
     * workspace mounts.
     */
    apiBase: string
    /** The host's fetch; it carries whatever the host's API requires. Read when the workspace mounts. */
    fetch: typeof fetch
    /** The profile shown. Another profile starts the workspace over, as if it had just mounted. */
    profileID: string
    /** Names the profile on the load-failure page, which otherwise says "this profile". */
    profileName?: string | undefined
    /** Keeps the workspace's view in the host's history. Read when the workspace mounts. */
    viewState: ViewStateAdapter
    /** Offers no change to the profile. A change starts the workspace over, as a new profile does. */
    readOnly?: boolean
    shortcuts?: boolean
    /** Starts a refresh the server reports as due. Read when the workspace mounts. */
    autoRefresh?: boolean
    /**
     * The view to open when the history entry has no view of its own, as a link to that view
     * would. Nothing changes when the first view is already that view. Read when the workspace
     * mounts.
     */
    initialView?: ViewID | undefined
    chrome?: boolean
    onlocation?: ((view: ViewID | undefined) => void) | undefined
    onreconnect?: (() => void) | undefined
    onamazonimport?: ((settings: { currency: string; scale: number }) => void) | undefined
    /**
     * Internal, for standalone moneyflow and tests: a controller that outlives the workspace,
     * which standalone moneyflow keeps across setup. Hosts do not pass it.
     */
    controller?: ViewController | undefined
  }

  let {
    apiBase,
    fetch: hostFetch,
    profileID,
    profileName,
    viewState,
    readOnly = false,
    shortcuts = true,
    autoRefresh = false,
    initialView,
    chrome = false,
    onlocation,
    onreconnect,
    onamazonimport,
    controller: supplied,
  }: Props = $props()
  const controller = resolveController()
  let shell = $state<AppShell>()
  let root = $state<HTMLElement>()
  let requested = $state<ViewID>()
  let reviewRequested = $state(false)
  /**
   * The host's view for a history entry without one, until the first view has loaded, history
   * moves to another entry, or the host asks for a view.
   */
  let initial = $state<ViewID>()

  function resolveController(): ViewController {
    if (supplied) return supplied
    return createViewController({
      client: createMoneyflowClient(apiBase, profileID, hostFetch),
      viewState,
      readOnly,
      autoRefresh,
    })
  }

  /** Shows a view. A view asked for while the workspace loads opens once it has loaded. */
  export function open(view: ViewID): void {
    // The host's request replaces the initial view, which as a maintain view would cover this one.
    initial = undefined
    if (shell && controller.projection) shell.open(view)
    else requested = view
  }

  /**
   * Opens the review of pending edits. A review asked for while the workspace loads opens once the
   * first view and the provider write status have loaded.
   */
  export function review(): void {
    reviewRequested = true
  }

  function importAmazon(): void {
    const settings = controller.projection?.amazon_settings
    if (settings) onamazonimport?.({ currency: settings.currency, scale: settings.scale })
  }

  $effect(() => {
    if (!shell || !controller.projection || initial === undefined) return
    const shown = navItems(controller.projection).explore.find((entry) => entry.active)?.id
    if (initial !== shown) shell.open(initial)
    initial = undefined
  })

  $effect(() => {
    if (!shell || !controller.projection || requested === undefined) return
    shell.open(requested)
    requested = undefined
  })

  // A running or finished write shows its status in place of the review, so the review waits for
  // the first write status check. Amazon profiles make none.
  $effect(() => {
    const projection = controller.projection
    if (!shell || !projection || !reviewRequested) return
    const write = controller.providerWrite.state
    if (projection.profile_kind !== 'amazon' && !write.status && write.phase === 'idle') return
    shell.review()
    reviewRequested = false
  })

  function pollProvider(): void {
    if (
      document.visibilityState === 'visible' &&
      controller.projection?.profile_kind !== 'amazon'
    ) {
      // A read-only host refuses provider status, so a read-only workspace never asks for it.
      if (!readOnly) void controller.provider.poll().catch(() => undefined)
      void controller.providerWrite.poll().catch(() => undefined)
    }
  }

  // The first status check starts with the first view, not after the view settles in its
  // currency, so a due refresh opens its dialog before the user works with stale data.
  let firstPoll = false
  $effect(() => {
    if (firstPoll || !controller.projection) return
    firstPoll = true
    pollProvider()
  })

  onMount(() => {
    const recheck = () => {
      void controller.recheck().catch(() => undefined)
      pollProvider()
    }
    const visible = () => {
      if (document.visibilityState === 'visible') recheck()
      else controller.flushHistory()
    }
    const leave = () => controller.flushHistory()
    // The first key press reveals the focus ring the grid takes on load. Keyboard mode lasts for
    // the page's life, so a workspace that mounts again after setup keeps it.
    if (keyboardUsed) root?.setAttribute('data-keyboard', '')
    const keyboard = () => {
      keyboardUsed = true
      root?.setAttribute('data-keyboard', '')
    }
    window.addEventListener('keydown', keyboard, { capture: true, once: true })
    const unlisten = viewState.listen(() => {
      // Back or Forward leaves the entry the host's initial view was for.
      initial = undefined
      void controller.restore({ state: viewState.state })
    })
    window.addEventListener('pagehide', leave)
    window.addEventListener('focus', recheck)
    document.addEventListener('visibilitychange', visible)
    const statusInterval = window.setInterval(pollProvider, 60_000)
    const writeStatusInterval = window.setInterval(() => {
      if (
        document.visibilityState === 'visible' &&
        controller.projection?.profile_kind !== 'amazon'
      ) {
        void controller.providerWrite.poll().catch(() => undefined)
      }
    }, 1_000)
    // A view the host asked for before the workspace mounted replaces the initial view too.
    if (!viewState.query && requested === undefined) initial = initialView
    // A controller kept across setup already holds a view, but Back or Forward during setup can
    // have moved history to another entry, so it shows the current entry's view again.
    const load = controller.projection
      ? controller.restore({ state: viewState.state })
      : controller.hydrate()
    void load.catch(() => undefined)
    return () => {
      window.clearInterval(statusInterval)
      window.clearInterval(writeStatusInterval)
      window.removeEventListener('keydown', keyboard, { capture: true })
      unlisten()
      window.removeEventListener('pagehide', leave)
      window.removeEventListener('focus', recheck)
      document.removeEventListener('visibilitychange', visible)
      controller.flushHistory()
      // A controller this workspace made goes with it; a kept one stays for the next workspace.
      if (!supplied) controller.stop()
    }
  })
</script>

{#snippet message(problem: NonNullable<ViewController['problem']>)}
  {#if problem.kind === 'invalid-view'}
    <section class="moneyflow-message" aria-labelledby="invalid-title">
      <svelte:element this={chrome ? 'h1' : 'h2'} id="invalid-title">
        This Moneyflow link cannot be opened
      </svelte:element>
      <p>The saved view is malformed or is not supported by this version.</p>
      <div class="moneyflow-actions">
        <Button onclick={() => viewState.go(-1)}>Back</Button>
        <Button tone="info" surface="solid" onclick={() => void controller.reset()}>
          Reset view
        </Button>
      </div>
    </section>
  {:else}
    <section class="moneyflow-message" aria-labelledby="error-title">
      <svelte:element this={chrome ? 'h1' : 'h2'} id="error-title">
        Couldn't load {profileName ?? 'this profile'}
      </svelte:element>
      <p>{problem.detail}</p>
      <Button tone="info" surface="solid" onclick={() => void controller.retry()}>Retry</Button>
    </section>
  {/if}
  <p class="kit-sr-only" aria-live="polite">{controller.announcement}</p>
{/snippet}

<div class="mf-views" bind:this={root}>
  {#if controller.problem && !controller.projection}
    {#if chrome}
      <div class="moneyflow-app">
        <TopBar ariaLabel="Moneyflow">
          {#snippet left()}
            <span class="moneyflow-brand">Moneyflow</span>
          {/snippet}
          {#snippet right()}
            <ThemeToggle size="sm" />
          {/snippet}
        </TopBar>
        <main class="moneyflow-main" aria-label="Moneyflow">
          {@render message(controller.problem)}
        </main>
        <StatusBar>
          {#snippet left()}
            <span role="status">{controller.loading ? 'Loading…' : ''}</span>
          {/snippet}
          {#snippet right()}
            <span>profile · {apiBase}</span>
          {/snippet}
        </StatusBar>
      </div>
    {:else}
      <div class="moneyflow-main">{@render message(controller.problem)}</div>
    {/if}
  {:else}
    <!-- The workspace lays itself out while the first view loads, so nothing swaps in after. -->
    <AppShell
      bind:this={shell}
      {controller}
      {chrome}
      globalKeys={shortcuts}
      {onlocation}
      {onreconnect}
      onamazonimport={importAmazon}
    />
  {/if}
</div>
