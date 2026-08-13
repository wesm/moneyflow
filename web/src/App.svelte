<script lang="ts">
  import { StatusBar, ThemeToggle, TopBar } from '@kenn-io/kit-ui'

  interface Props {
    basePath: string
  }

  let { basePath }: Props = $props()
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
    <section class="moneyflow-loading" aria-labelledby="loading-title">
      <p class="moneyflow-eyebrow">Read-only fixture</p>
      <h1 id="loading-title">Loading financial view…</h1>
      <p>Preparing the keyboard-first transaction workspace.</p>
    </section>
  </main>

  <StatusBar>
    {#snippet left()}
      <span role="status">Loading fixture data…</span>
    {/snippet}
    {#snippet right()}
      <span>read only · {basePath}</span>
    {/snippet}
  </StatusBar>
</div>
