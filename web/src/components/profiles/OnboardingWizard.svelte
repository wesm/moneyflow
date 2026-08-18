<script lang="ts" module>
  import type { OnboardingStatus } from '../../lib/controller/onboarding.svelte'

  type Progress = NonNullable<OnboardingStatus['progress']>

  function progressTitle(phase: string | undefined): string {
    if (phase === 'verifying') return 'Verifying Monarch data'
    if (phase === 'folding' || phase === 'importing') return 'Importing Monarch data'
    return 'Fetching Monarch data'
  }

  function progressDescription(progress: Progress | undefined): string {
    if (!progress) return 'Preparing the provider import.'
    return `${progressTitle(progress.phase)}: ${progress.fetched.toLocaleString()} of ${progress.total.toLocaleString()} ${progress.partition || 'transactions'}.`
  }

  function formatElapsed(milliseconds: number): string {
    const seconds = milliseconds / 1000
    return `${seconds.toLocaleString(undefined, { maximumFractionDigits: 1 })} seconds`
  }
</script>

<script lang="ts">
  import { Button, Card, Spinner, StatusBar, TextInput, ThemeToggle, TopBar } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'

  import type { OnboardingController } from '../../lib/controller/onboarding.svelte'

  interface Props {
    controller: OnboardingController
    oncomplete: () => void
    oncancel: () => void
    onoffline?: () => void
  }

  let { controller, oncomplete, oncancel, onoffline }: Props = $props()
  let currency = $state('USD')
  let scale = $state('2')
  let accountPassword = $state('')
  let email = $state('')
  let password = $state('')
  let totpSecret = $state('')
  let confirmation = $state('')
  let validation = $state('')
  const snapshot = $derived(controller.state.snapshot)
  const problem = $derived(controller.state.problem)
  const progress = $derived(snapshot?.progress)

  $effect(() => {
    if (snapshot?.settings) {
      currency = snapshot.settings.currency
      scale = snapshot.settings.scale.toString(10)
    }
    if (snapshot?.state === 'complete') oncomplete()
    if (snapshot?.state === 'canceled') oncancel()
  })

  onMount(() => {
    if (!controller.state.snapshot) void controller.start()
    return () => controller.destroy()
  })

  async function submitSettings(): Promise<void> {
    const parsed = Number(scale)
    const normalizedCurrency = currency.trim().toUpperCase()
    if (
      !/^[A-Z]{3}$/.test(normalizedCurrency) ||
      !Number.isInteger(parsed) ||
      parsed < 0 ||
      parsed > 9
    ) {
      validation = 'Enter a three-letter currency and a scale from 0 to 9.'
      return
    }
    validation = ''
    await controller.confirmSettings(normalizedCurrency, parsed)
  }

  async function submitUnlock(): Promise<void> {
    if (!accountPassword) {
      validation = 'Enter the Moneyflow account password.'
      return
    }
    const submitted = accountPassword
    accountPassword = ''
    validation = ''
    await controller.unlock(submitted)
    accountPassword = ''
  }

  async function submitCredentials(): Promise<void> {
    if (!email.trim() || !password || !totpSecret.trim() || !accountPassword) {
      validation = 'Complete every credential field.'
      return
    }
    if (accountPassword !== confirmation) {
      validation = 'Moneyflow account passwords do not match.'
      return
    }
    const submitted = {
      email: email.trim(),
      password,
      totp_secret: totpSecret.trim(),
      account_password: accountPassword,
      confirmation,
    }
    email = ''
    password = ''
    totpSecret = ''
    accountPassword = ''
    confirmation = ''
    validation = ''
    await controller.submitCredentials(submitted)
    email = ''
    password = ''
    totpSecret = ''
    accountPassword = ''
    confirmation = ''
  }

  async function cancel(): Promise<void> {
    await controller.cancel()
  }
</script>

<div class="moneyflow-app onboarding-wizard">
  <TopBar ariaLabel="Moneyflow setup">
    {#snippet left()}<span class="moneyflow-brand">Moneyflow setup</span>{/snippet}
    {#snippet right()}<ThemeToggle size="sm" />{/snippet}
  </TopBar>
  <main class="profile-main" aria-label="Profile onboarding">
    <section class="profile-panel" aria-labelledby="onboarding-title">
      <p class="moneyflow-eyebrow">Monarch Money</p>
      {#if problem}
        <h1 id="onboarding-title">Profile setup was interrupted</h1>
        <Card
          level="inset"
          title={problem.kind === 'expired' ? 'Setup expired' : 'Setup did not start'}
          ><p>{problem.message}</p></Card
        >
        <div class="profile-actions">
          <Button onclick={oncancel}>Back to profiles</Button>
          <Button tone="info" surface="solid" onclick={() => void controller.restart()}>
            Retry setup
          </Button>
        </div>
      {:else if !snapshot || ['inspect', 'validate_session'].includes(snapshot.state)}
        <h1 id="onboarding-title">Checking saved session</h1>
        <div class="onboarding-working">
          <Spinner label="Checking saved Monarch session" />
          <p>Looking for a reusable encrypted session before asking for credentials.</p>
        </div>
      {:else if snapshot.state === 'settings_required'}
        <h1 id="onboarding-title">Confirm import settings</h1>
        <p>Moneyflow stores exact integer minor units. Confirm the currency and decimal scale.</p>
        <form
          class="profile-form"
          onsubmit={(event) => {
            event.preventDefault()
            void submitSettings()
          }}
        >
          <label for="onboarding-currency">Currency</label>
          <TextInput id="onboarding-currency" bind:value={currency} block autocomplete="off" />
          <label for="onboarding-scale">Minor-unit scale</label>
          <TextInput id="onboarding-scale" bind:value={scale} block autocomplete="off" />
          {#if validation}<p role="alert" class="editing-error">{validation}</p>{/if}
          <div class="profile-actions">
            <Button type="button" onclick={() => void cancel()}>Cancel</Button><Button
              type="submit"
              tone="info"
              surface="solid">Continue with {currency || 'USD'} / {scale || '2'}</Button
            >
          </div>
        </form>
      {:else if snapshot.state === 'unlock_required'}
        <h1 id="onboarding-title">Unlock saved credentials</h1>
        <p>
          Enter the Moneyflow account password used to protect the local Monarch credential vault.
        </p>
        <form
          class="profile-form"
          onsubmit={(event) => {
            event.preventDefault()
            void submitUnlock()
          }}
        >
          <label for="onboarding-account-password">Moneyflow account password</label>
          <TextInput
            id="onboarding-account-password"
            type="password"
            bind:value={accountPassword}
            block
            autofocus
            autocomplete="current-password"
          />
          {#if validation}<p role="alert" class="editing-error">{validation}</p>{/if}
          <div class="profile-actions">
            <Button type="button" onclick={() => void cancel()}>Cancel</Button><Button
              type="submit"
              tone="info"
              surface="solid">Unlock</Button
            >
          </div>
        </form>
      {:else if snapshot.state === 'credentials_required'}
        <h1 id="onboarding-title">Connect Monarch Money</h1>
        <p>
          Credentials are sent only to this Moneyflow server and saved only in the encrypted local
          vault.
        </p>
        <form
          class="profile-form"
          onsubmit={(event) => {
            event.preventDefault()
            void submitCredentials()
          }}
        >
          <label for="onboarding-email">Monarch email</label>
          <TextInput
            id="onboarding-email"
            type="email"
            bind:value={email}
            block
            autofocus
            autocomplete="username"
          />
          <label for="onboarding-password">Monarch password</label>
          <TextInput
            id="onboarding-password"
            type="password"
            bind:value={password}
            block
            autocomplete="current-password"
          />
          <label for="onboarding-totp">TOTP secret</label>
          <TextInput
            id="onboarding-totp"
            type="password"
            bind:value={totpSecret}
            block
            autocomplete="off"
          />
          <label for="onboarding-account-password">Moneyflow account password</label>
          <TextInput
            id="onboarding-account-password"
            type="password"
            bind:value={accountPassword}
            block
            autocomplete="new-password"
          />
          <label for="onboarding-confirmation">Confirm Moneyflow account password</label>
          <TextInput
            id="onboarding-confirmation"
            type="password"
            bind:value={confirmation}
            block
            autocomplete="new-password"
          />
          {#if validation}<p role="alert" class="editing-error">{validation}</p>{/if}
          <div class="profile-actions">
            <Button type="button" onclick={() => void cancel()}>Cancel</Button><Button
              type="submit"
              tone="info"
              surface="solid">Connect</Button
            >
          </div>
        </form>
      {:else if ['authenticating', 'importing'].includes(snapshot.state)}
        <h1 id="onboarding-title">
          {snapshot.state === 'authenticating'
            ? 'Authenticating with Monarch'
            : progressTitle(progress?.phase)}
        </h1>
        <div class="onboarding-working">
          <Spinner label="Provider setup in progress" />
          <p>
            {progressDescription(progress)}
          </p>
        </div>
        <Button onclick={() => void cancel()}>Cancel</Button>
      {:else if snapshot.state === 'local_only'}
        <h1 id="onboarding-title">This profile contains local data</h1>
        <p>Provider setup cannot replace a non-pristine local profile. You can open it offline.</p>
        <div class="profile-actions">
          <Button onclick={oncancel}>Back</Button>{#if onoffline}<Button
              tone="info"
              surface="solid"
              onclick={onoffline}>Open Offline</Button
            >{/if}
        </div>
      {:else if snapshot.state === 'identity_mismatch'}
        <h1 id="onboarding-title">Different Monarch account</h1>
        <p>This profile is bound to a different Monarch account. Local data was not changed.</p>
        <div class="profile-actions">
          <Button onclick={() => void cancel()}>Cancel</Button><Button
            tone="info"
            surface="solid"
            onclick={() => void controller.reauthenticate()}>Re-enter credentials</Button
          >
        </div>
      {:else if snapshot.state === 'failed'}
        <h1 id="onboarding-title">Provider setup needs attention</h1>
        <Card level="inset" title="Setup failed"
          ><p>{snapshot.failure?.message ?? 'The provider setup failed.'}</p></Card
        >
        <div class="profile-actions">
          <Button onclick={() => void cancel()}>Cancel</Button>
          {#if snapshot.failure?.can_reenter}<Button
              onclick={() => void controller.reauthenticate()}
            >
              Re-enter credentials
            </Button>{/if}
          {#if snapshot.failure?.can_retry}<Button
              tone="info"
              surface="solid"
              onclick={() => void controller.retry()}
            >
              Retry
            </Button>{/if}
        </div>
      {:else}
        <h1 id="onboarding-title">Profile setup complete</h1>
        <p>Opening the keyboard-first financial workspace…</p>
      {/if}
      {#if progress}
        <p role="status" aria-live="polite">
          {progress.fetched.toLocaleString()} of {progress.total.toLocaleString()}
          {progress.partition || 'transactions'} · {formatElapsed(progress.elapsed_ms)}
        </p>
      {/if}
      <p class="kit-sr-only" aria-live="polite">{controller.state.announcement}</p>
    </section>
  </main>
  <StatusBar>
    {#snippet left()}<span role={progress ? undefined : 'status'}
        >{controller.state.announcement}</span
      >{/snippet}
    {#snippet right()}<span
        >{snapshot?.state ?? 'starting'} · secrets never enter browser storage</span
      >{/snippet}
  </StatusBar>
</div>
