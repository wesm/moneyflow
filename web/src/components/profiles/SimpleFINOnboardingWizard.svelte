<script lang="ts">
  import { Button, Spinner, StatusBar, TextInput, ThemeToggle, TopBar } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'
  import type { SimpleFINOnboardingController } from '../../lib/controller/simplefin-onboarding.svelte'

  interface Props {
    controller: SimpleFINOnboardingController
    oncomplete: () => void
    oncancel: () => void
    onoffline?: () => void
  }
  let { controller, oncomplete, oncancel, onoffline }: Props = $props()
  let currency = $state('USD')
  let scale = $state('2')
  let settingsReady = $state(false)
  let input = $state('')
  let validation = $state('')
  let elapsed = $state(0)
  const snapshot = $derived(controller.state.snapshot)
  const needsInput = $derived(
    snapshot?.state === 'credentials_required' || snapshot?.failure?.can_reenter,
  )

  $effect(() => {
    if (snapshot?.state === 'complete') oncomplete()
    if (snapshot?.state === 'canceled' && !snapshot.failure) oncancel()
  })
  onMount(() => {
    if (!controller.state.snapshot) void controller.start()
    const started = Date.now()
    const timer = setInterval(() => (elapsed = Math.floor((Date.now() - started) / 1000)), 1000)
    return () => {
      clearInterval(timer)
      controller.destroy()
    }
  })
  async function submit(): Promise<void> {
    const code = currency.trim().toUpperCase()
    if (!/^[A-Z]{3}$/.test(code) || !/^[0-9]$/.test(scale)) {
      validation = 'Use a three-letter currency code and 0–9 decimal places.'
      return
    }
    validation = ''
    if (!settingsReady) {
      currency = code
      settingsReady = true
      return
    }
    if (!input || new TextEncoder().encode(input).length > 8192) {
      validation = 'Paste a setup token or Access URL (at most 8 KiB).'
      return
    }
    const credential = input
    input = ''
    await controller.connect(credential, { currency: code, scale: Number(scale) })
  }
</script>

<div class="moneyflow-app onboarding-wizard">
  <TopBar ariaLabel="Moneyflow setup">
    {#snippet left()}<span class="moneyflow-brand">Moneyflow setup</span>{/snippet}
    {#snippet right()}<ThemeToggle size="sm" />{/snippet}
  </TopBar>
  <main class="profile-main" aria-label="SimpleFIN profile onboarding">
    <section class="profile-panel" aria-labelledby="simplefin-title">
      <h1 id="simplefin-title">Connect SimpleFIN <small>(experimental)</small></h1>
      <p>Edits stay in Moneyflow; your bank data is never changed.</p>
      {#if controller.state.problem}
        <p role="alert">{controller.state.problem.message}</p>
        <div class="profile-actions">
          <Button onclick={oncancel}>Back to profiles</Button>
          <Button onclick={() => void controller.restart()}>Retry setup</Button>
        </div>
      {:else if snapshot?.state === 'canceled' && snapshot.failure}
        <h2>Setup stopped</h2>
        <p role="alert">{snapshot.failure.message}</p>
        <div class="profile-actions">
          <Button onclick={oncancel}>Back to profiles</Button>
        </div>
      {:else if needsInput}
        {#if snapshot?.failure}<p role="alert">{snapshot.failure.message}</p>{/if}
        <form
          class="profile-form"
          onsubmit={(event) => {
            event.preventDefault()
            void submit()
          }}
        >
          {#if !settingsReady}
            <p>Use the currency of the accounts connected through SimpleFIN.</p>
            <label for="simplefin-currency">Currency</label>
            <TextInput
              id="simplefin-currency"
              bind:value={currency}
              block
              autofocus
              autocomplete="off"
            />
            <label for="simplefin-scale">Decimal places</label>
            <TextInput id="simplefin-scale" bind:value={scale} block />
            <p>For USD: 2 decimal places, e.g. 12.34.</p>
          {:else}
            <p>Import as {currency} with {scale} decimal places.</p>
            <label for="simplefin-input">Setup token or Access URL</label>
            <TextInput
              id="simplefin-input"
              type="password"
              bind:value={input}
              block
              autofocus
              autocomplete="off"
            />
            <p>
              Paste from SimpleFIN Bridge. The connection is saved on this Moneyflow server before
              importing.
            </p>
          {/if}
          {#if validation}<p role="alert" class="editing-error">{validation}</p>{/if}
          <div class="profile-actions">
            <Button
              type="button"
              disabled={controller.state.busy}
              onclick={() => void controller.cancel()}>Cancel</Button
            >
            {#if settingsReady}<Button type="button" onclick={() => (settingsReady = false)}
                >Back</Button
              >{/if}
            <Button type="submit" tone="info" surface="solid" disabled={controller.state.busy}
              >{settingsReady ? 'Connect' : 'Continue'}</Button
            >
          </div>
        </form>
      {:else if snapshot?.state === 'failed' || snapshot?.state === 'identity_mismatch'}
        <h2>Setup needs attention</h2>
        <p role="alert">{snapshot.failure?.message}</p>
        {#if snapshot.next_eligible}<p>
            Next import available: {new Date(snapshot.next_eligible).toLocaleString()}.
          </p>{/if}
        <div class="profile-actions">
          <Button disabled={controller.state.busy} onclick={() => void controller.cancel()}
            >Back to profiles</Button
          >
          {#if onoffline}<Button onclick={onoffline}>Open cached data</Button>{/if}
          {#if snapshot.failure?.can_retry}<Button
              disabled={controller.state.busy}
              tone="info"
              surface="solid"
              onclick={() => void controller.retry()}>Retry</Button
            >{/if}
        </div>
      {:else}
        <div class="onboarding-working">
          <Spinner label="SimpleFIN setup in progress" />
          <p>{controller.state.announcement || 'Checking saved connection…'}</p>
        </div>
        {#if snapshot?.state === 'importing'}
          <p>
            {snapshot.progress.fetched.toLocaleString()} transactions fetched · {elapsed}s elapsed
          </p>
          <p>Local edits and existing transactions are kept unchanged.</p>
        {/if}
        <Button disabled={controller.state.busy} onclick={() => void controller.cancel()}
          >Cancel</Button
        >
      {/if}
    </section>
  </main>
  <StatusBar
    >{#snippet left()}<span role="status">{controller.state.announcement}</span
      >{/snippet}</StatusBar
  >
</div>
