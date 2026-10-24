<script lang="ts" module>
  import type { OnboardingStatus } from '../../lib/controller/onboarding.svelte'
  import { providerName } from '../../lib/provider-name'

  type Progress = NonNullable<OnboardingStatus['progress']>

  function progressTitle(phase: string | undefined, provider: string): string {
    if (phase === 'verifying') return `Verifying ${provider} data`
    if (phase === 'folding' || phase === 'importing') return `Importing ${provider} data`
    return `Fetching ${provider} data`
  }

  function progressDescription(progress: Progress | undefined, provider: string): string {
    if (!progress) return 'Preparing the provider import.'
    return `${progressTitle(progress.phase, provider)}: ${progress.fetched.toLocaleString()} of ${progress.total.toLocaleString()} ${progress.partition || 'transactions'}.`
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
    /** Frames the wizard as a page, with a top bar, theme button and status bar. */
    chrome?: boolean
  }

  let { controller, oncomplete, oncancel, onoffline, chrome = true }: Props = $props()
  let currency = $state('USD')
  let scale = $state('2')
  let accountPassword = $state('')
  let email = $state('')
  let password = $state('')
  let totpSecret = $state('')
  let confirmation = $state('')
  let accessToken = $state('')
  let validation = $state('')
  const snapshot = $derived(controller.state.snapshot)
  const problem = $derived(controller.state.problem)
  const progress = $derived(snapshot?.progress)
  const provider = $derived(providerName(snapshot?.provider_kind))

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
      validation = 'Enter a three-letter currency and 0 to 9 decimal places.'
      return
    }
    validation = ''
    await controller.confirmSettings(normalizedCurrency, parsed)
  }

  async function submitUnlock(): Promise<void> {
    if (!accountPassword) {
      validation = 'Enter the account password.'
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
      validation = 'The account passwords do not match.'
      return
    }
    const submitted = {
      email: email.trim(),
      password,
      totp_secret: totpSecret.trim(),
      account_password: accountPassword,
      confirmation,
    }
    password = ''
    totpSecret = ''
    accountPassword = ''
    confirmation = ''
    validation = ''
    await controller.submitCredentials(submitted)
    password = ''
    totpSecret = ''
    accountPassword = ''
    confirmation = ''
  }

  async function submitYNABCredentials(): Promise<void> {
    if (!accessToken || !accountPassword) {
      validation = 'Complete every credential field.'
      return
    }
    if (accountPassword !== confirmation) {
      validation = 'The account passwords do not match.'
      return
    }
    const submitted = {
      access_token: accessToken,
      account_password: accountPassword,
      confirmation,
    }
    accessToken = ''
    accountPassword = ''
    confirmation = ''
    validation = ''
    await controller.submitYNABCredentials(submitted)
    accessToken = ''
    accountPassword = ''
    confirmation = ''
  }

  async function cancel(): Promise<void> {
    await controller.cancel()
  }
</script>

<div class="moneyflow-app onboarding-wizard">
  {#if chrome}
    <TopBar ariaLabel="Moneyflow setup">
      {#snippet left()}<span class="moneyflow-brand">Moneyflow setup</span>{/snippet}
      {#snippet right()}<ThemeToggle size="sm" />{/snippet}
    </TopBar>
  {/if}
  <svelte:element
    this={chrome ? 'main' : 'div'}
    class="profile-main"
    aria-label={chrome ? 'Profile onboarding' : undefined}
  >
    <section class="profile-panel" aria-labelledby="onboarding-title">
      <p class="moneyflow-eyebrow">{provider}</p>
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
          <Spinner label={`Checking saved ${provider} credentials`} />
          <p>Looking for a reusable encrypted session before asking for credentials.</p>
        </div>
        {#if snapshot}<Button onclick={() => void cancel()}>Cancel</Button>{/if}
      {:else if snapshot.state === 'settings_required'}
        <h1 id="onboarding-title">Confirm import settings</h1>
        <p>
          Confirm the currency and how many decimal places its amounts use, such as USD and 2. They
          cannot change after setup.
        </p>
        <form
          class="profile-form"
          onsubmit={(event) => {
            event.preventDefault()
            void submitSettings()
          }}
        >
          <label for="onboarding-currency">Currency</label>
          <TextInput
            id="onboarding-currency"
            bind:value={currency}
            block
            readonly={snapshot.provider_kind === 'ynab'}
            autocomplete="off"
          />
          <label for="onboarding-scale">Decimal places</label>
          <TextInput
            id="onboarding-scale"
            bind:value={scale}
            block
            readonly={snapshot.provider_kind === 'ynab'}
            autocomplete="off"
          />
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
        <p>Enter the account password you chose when you set up this {provider} profile.</p>
        <form
          class="profile-form"
          onsubmit={(event) => {
            event.preventDefault()
            void submitUnlock()
          }}
        >
          <label for="onboarding-account-password">Account password</label>
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
      {:else if snapshot.state === 'credentials_required' && snapshot.provider_kind === 'ynab'}
        <h1 id="onboarding-title">Connect YNAB</h1>
        <p>The token goes only to this server, which saves it encrypted.</p>
        <form
          class="profile-form"
          onsubmit={(event) => {
            event.preventDefault()
            void submitYNABCredentials()
          }}
        >
          <label for="onboarding-ynab-token">YNAB personal access token</label>
          <TextInput
            id="onboarding-ynab-token"
            type="password"
            bind:value={accessToken}
            block
            autofocus
            autocomplete="off"
            ariaDescribedby="onboarding-ynab-token-hint"
          />
          <p id="onboarding-ynab-token-hint" class="profile-hint">
            Create one in YNAB under Account Settings, then Developer Settings.
          </p>
          <label for="onboarding-account-password">Account password</label>
          <TextInput
            id="onboarding-account-password"
            type="password"
            bind:value={accountPassword}
            block
            autocomplete="new-password"
            ariaDescribedby="onboarding-account-password-hint"
          />
          <p id="onboarding-account-password-hint" class="profile-hint">
            Choose a new password. It encrypts the saved sign-in, and you enter it to unlock this
            profile later.
          </p>
          <label for="onboarding-confirmation">Confirm account password</label>
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
      {:else if snapshot.state === 'credentials_required'}
        <h1 id="onboarding-title">Connect Monarch Money</h1>
        <p>Your sign-in goes only to this server, which saves it encrypted.</p>
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
          <label for="onboarding-totp">Authenticator setup key</label>
          <TextInput
            id="onboarding-totp"
            type="password"
            bind:value={totpSecret}
            block
            autocomplete="off"
            ariaDescribedby="onboarding-totp-hint"
          />
          <p id="onboarding-totp-hint" class="profile-hint">
            The text key behind the QR code in Monarch Settings &gt; Security, not a six-digit code.
            Monarch sign-in must use an authenticator app.
          </p>
          <label for="onboarding-account-password">Account password</label>
          <TextInput
            id="onboarding-account-password"
            type="password"
            bind:value={accountPassword}
            block
            autocomplete="new-password"
            ariaDescribedby="onboarding-account-password-hint"
          />
          <p id="onboarding-account-password-hint" class="profile-hint">
            Choose a new password. It encrypts the saved sign-in, and you enter it to unlock this
            profile later.
          </p>
          <label for="onboarding-confirmation">Confirm account password</label>
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
      {:else if snapshot.state === 'remote_profile_required'}
        <h1 id="onboarding-title">Choose a YNAB budget</h1>
        <p>The profile stays tied to the budget you choose.</p>
        <div class="profile-list" role="list">
          {#each snapshot.remote_profiles ?? [] as choice (choice.choice_id)}
            <Card
              title={choice.display_name}
              meta={choice.last_modified ? `Updated ${choice.last_modified}` : 'Available'}
              onclick={() => void controller.selectRemoteProfile(choice.choice_id)}
            />
          {/each}
        </div>
        <div class="profile-actions"><Button onclick={() => void cancel()}>Cancel</Button></div>
      {:else if ['authenticating', 'importing'].includes(snapshot.state)}
        <h1 id="onboarding-title">
          {snapshot.state === 'authenticating'
            ? `Authenticating with ${provider}`
            : progressTitle(progress?.phase, provider)}
        </h1>
        <div class="onboarding-working">
          <Spinner label="Provider setup in progress" />
          <p>
            {progressDescription(progress, provider)}
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
        <h1 id="onboarding-title">Different {provider} account</h1>
        <p>
          This profile is bound to a different {provider} account. Local data was not changed.
        </p>
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
        <p>Opening the profile…</p>
      {/if}
      {#if controller.state.failure && !problem}
        <p role="alert" class="editing-error">{controller.state.failure}</p>
      {/if}
      {#if progress}
        <p role="status" aria-live="polite">
          {progress.fetched.toLocaleString()} of {progress.total.toLocaleString()}
          {progress.partition || 'transactions'} · {formatElapsed(progress.elapsed_ms)}
        </p>
      {/if}
      <p class="kit-sr-only" aria-live="polite">{controller.state.announcement}</p>
    </section>
  </svelte:element>
  {#if chrome}
    <StatusBar>
      {#snippet left()}<span role={progress ? undefined : 'status'}
          >{controller.state.announcement}</span
        >{/snippet}
      {#snippet right()}<span
          >{snapshot?.state ?? 'starting'} · secrets never enter browser storage</span
        >{/snippet}
    </StatusBar>
  {/if}
</div>
