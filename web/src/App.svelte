<script lang="ts">
  import { Button, StatusBar, ThemeToggle, TopBar } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'

  import { createMoneyflowClient } from './lib/api/client'
  import {
    createViewController,
    type ViewController,
  } from './lib/controller/view-controller.svelte'

  interface Props {
    basePath: string
    controller?: ViewController
  }

  let { basePath, controller: suppliedController }: Props = $props()
  const controller = resolveController()

  function resolveController(): ViewController {
    return (
      suppliedController ??
      createViewController({ basePath, client: createMoneyflowClient(basePath) })
    )
  }

  onMount(() => {
    const restore = (event: PopStateEvent) => void controller.restore(event)
    window.addEventListener('popstate', restore)
    void controller.hydrate()
    return () => window.removeEventListener('popstate', restore)
  })
</script>

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
    {:else if controller.projection}
      <section class="moneyflow-loaded" aria-labelledby="view-title">
        <p class="moneyflow-eyebrow">Read-only fixture</p>
        <h1 id="view-title">{controller.projection.breadcrumb_text}</h1>
        <p>{controller.projection.total_rows} results ready for keyboard navigation.</p>
      </section>
    {:else}
      <section class="moneyflow-loading" aria-labelledby="loading-title">
        <p class="moneyflow-eyebrow">Read-only fixture</p>
        <h1 id="loading-title">Loading financial view…</h1>
        <p>Preparing the keyboard-first transaction workspace.</p>
      </section>
    {/if}
    <p class="kit-sr-only" aria-live="polite">{controller.announcement}</p>
  </main>

  <StatusBar>
    {#snippet left()}
      <span role="status">
        {controller.loading ? 'Loading fixture data…' : (controller.projection?.status ?? 'Ready')}
      </span>
    {/snippet}
    {#snippet right()}
      <span>read only · {basePath}</span>
    {/snippet}
  </StatusBar>
</div>
