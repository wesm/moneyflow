<script lang="ts">
  import { Button, StatusBar, ThemeToggle, TopBar } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'

  import { createMoneyflowClient } from './lib/api/client'
  import AppShell from './components/AppShell.svelte'
  import {
    createViewController,
    type ViewController,
  } from './lib/controller/view-controller.svelte'
  import { profileApplicationPath } from './lib/controller/routing'

  interface Props {
    basePath: string
    profileID?: string
    controller?: ViewController
  }

  let { basePath, profileID, controller: suppliedController }: Props = $props()
  const controller = resolveController()

  function resolveController(): ViewController {
    if (suppliedController) return suppliedController
    if (!profileID) throw new Error('Moneyflow profile route is missing.')
    return createViewController({
      basePath: profileApplicationPath(basePath, profileID),
      client: createMoneyflowClient(basePath, profileID),
    })
  }

  onMount(() => {
    const restore = (event: PopStateEvent) => void controller.restore(event)
    const pollProvider = () => {
      if (document.visibilityState === 'visible') {
        void controller.provider.poll().catch(() => undefined)
      }
    }
    const recheck = () => {
      void controller.recheck().catch(() => undefined)
      pollProvider()
    }
    const visible = () => {
      if (document.visibilityState === 'visible') recheck()
    }
    window.addEventListener('popstate', restore)
    window.addEventListener('focus', recheck)
    document.addEventListener('visibilitychange', visible)
    const statusInterval = window.setInterval(pollProvider, 60_000)
    void controller
      .hydrate()
      .then(pollProvider)
      .catch(() => undefined)
    return () => {
      window.clearInterval(statusInterval)
      window.removeEventListener('popstate', restore)
      window.removeEventListener('focus', recheck)
      document.removeEventListener('visibilitychange', visible)
    }
  })
</script>

{#if controller.projection}
  <AppShell {controller} />
{:else}
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
      {#if controller.problem?.kind === 'invalid-view'}
        <section class="moneyflow-message" aria-labelledby="invalid-title">
          <p class="moneyflow-eyebrow">Invalid view</p>
          <h1 id="invalid-title">This Moneyflow link cannot be opened</h1>
          <p>The saved view is malformed or is not supported by this version.</p>
          <div class="moneyflow-actions">
            <Button onclick={() => history.back()}>Back</Button>
            <Button tone="info" surface="solid" onclick={() => void controller.reset()}>
              Reset view
            </Button>
          </div>
        </section>
      {:else if controller.problem?.kind === 'request' && controller.projection === undefined}
        <section class="moneyflow-message" aria-labelledby="error-title">
          <p class="moneyflow-eyebrow">Connection problem</p>
          <h1 id="error-title">The financial view did not load</h1>
          <p>Check that the Moneyflow server is reachable, then try again.</p>
          <Button tone="info" surface="solid" onclick={() => void controller.retry()}>Retry</Button>
        </section>
      {:else}
        <section class="moneyflow-loading" aria-labelledby="loading-title">
          <p class="moneyflow-eyebrow">Local profile</p>
          <h1 id="loading-title">Loading financial view…</h1>
          <p>Preparing the keyboard-first transaction workspace.</p>
        </section>
      {/if}
      <p class="kit-sr-only" aria-live="polite">{controller.announcement}</p>
    </main>

    <StatusBar>
      {#snippet left()}
        <span role="status">
          {controller.loading
            ? 'Loading profile data…'
            : (controller.projection?.status ?? 'Ready')}
        </span>
      {/snippet}
      {#snippet right()}
        <span>profile · {basePath}</span>
      {/snippet}
    </StatusBar>
  </div>
{/if}
