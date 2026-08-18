<script lang="ts">
  import { Button, StatusBar, ThemeToggle, TopBar } from '@kenn-io/kit-ui'
  import { onMount, tick } from 'svelte'

  import { createMoneyflowClient } from './lib/api/client'
  import { createCatalogClient, type ProfileSummary } from './lib/api/catalog-client'
  import AppShell from './components/AppShell.svelte'
  import OnboardingWizard from './components/profiles/OnboardingWizard.svelte'
  import ProfileSelector from './components/profiles/ProfileSelector.svelte'
  import { createCatalogController, type CatalogController } from './lib/controller/catalog.svelte'
  import {
    createOnboardingController,
    createOnboardingTransport,
    type OnboardingController,
  } from './lib/controller/onboarding.svelte'
  import {
    createViewController,
    type ViewController,
  } from './lib/controller/view-controller.svelte'
  import { profileApplicationPath } from './lib/controller/routing'

  interface Props {
    basePath: string
    profileID?: string
    controller?: ViewController
    catalog?: CatalogController
    onnavigate?: (profileID: string) => void
  }

  let {
    basePath,
    profileID,
    controller: suppliedController,
    catalog: suppliedCatalog,
    onnavigate,
  }: Props = $props()
  const controller = resolveController()
  const catalog = resolveCatalog()
  let onboarding = $state<OnboardingController | undefined>()
  let onboardingProfileID = $state<string | undefined>()
  let createdOnboardingProfileID = $state<string | undefined>()
  let closingOnboarding = $state(false)

  function resolveController(): ViewController | undefined {
    if (suppliedController) return suppliedController
    if (!profileID) return undefined
    return createViewController({
      basePath: profileApplicationPath(basePath, profileID),
      client: createMoneyflowClient(basePath, profileID),
    })
  }

  function resolveCatalog(): CatalogController | undefined {
    if (suppliedCatalog) return suppliedCatalog
    if (profileID || suppliedController) return undefined
    return createCatalogController({ client: createCatalogClient(basePath) })
  }

  async function canonicalID(
    selector: string,
    providerKind?: 'monarch' | 'local',
  ): Promise<string | undefined> {
    if (!catalog) return selector
    const profile = catalog.state.profiles.find(
      (candidate) => candidate.id === selector || candidate.key === selector,
    )
    if (!profile) return undefined
    try {
      return await catalog.canonicalID(profile, providerKind)
    } catch {
      catalog.announce('The selected profile could not be activated.')
      return undefined
    }
  }

  async function navigate(selector: string): Promise<void> {
    const id = await canonicalID(selector)
    if (!id) return
    if (onnavigate) {
      onnavigate(id)
      return
    }
    globalThis.location.assign(
      `${profileApplicationPath(basePath, id)}${globalThis.location.search}`,
    )
  }

  async function setup(selector: string): Promise<void> {
    const id = await canonicalID(selector, 'monarch')
    if (!id) return
    onboarding?.destroy()
    if (createdOnboardingProfileID !== id) createdOnboardingProfileID = undefined
    onboardingProfileID = id
    onboarding = createOnboardingController({
      profileID: id,
      transport: createOnboardingTransport(basePath, id),
    })
  }

  async function closeOnboarding(): Promise<void> {
    if (closingOnboarding) return
    closingOnboarding = true
    onboarding?.destroy()
    const createdProfileID = createdOnboardingProfileID
    createdOnboardingProfileID = undefined
    if (catalog && createdProfileID) await catalog.cancelNew(createdProfileID)
    else if (catalog) await catalog.load()
    onboarding = undefined
    onboardingProfileID = undefined
    closingOnboarding = false
    if (catalog) return
    if (controller) {
      void tick().then(() => {
        requestAnimationFrame(() => {
          document
            .querySelector<HTMLElement>('[role="grid"][aria-label="Financial results"]')
            ?.focus({ preventScroll: true })
        })
      })
    }
  }

  async function createProfile(
    name: string,
    provider: 'monarch' | 'local',
  ): Promise<ProfileSummary | undefined> {
    if (!catalog) return undefined
    const created = await catalog.create(name, provider)
    if (provider === 'monarch' && created?.id) createdOnboardingProfileID = created.id
    return created
  }

  function completeOnboarding(): void {
    const id = onboardingProfileID
    createdOnboardingProfileID = undefined
    if (id) void navigate(id)
  }

  async function recover(id: string, confirmed: boolean): Promise<void> {
    if (!catalog) return
    const response = await catalog.recovery(id, confirmed)
    if (confirmed && response?.recreated) await setup(id)
  }

  onMount(() => {
    if (catalog) {
      void catalog.load()
      return () => onboarding?.destroy()
    }
    if (!controller) return
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

{#if onboarding && onboardingProfileID}
  <OnboardingWizard
    controller={onboarding}
    oncomplete={completeOnboarding}
    oncancel={() => void closeOnboarding()}
    onoffline={() => void navigate(onboardingProfileID!)}
  />
{:else if catalog}
  <ProfileSelector
    profiles={catalog.state.profiles}
    loading={catalog.state.loading}
    announcement={catalog.state.announcement}
    problem={catalog.state.problem}
    recovery={catalog.state.recovery}
    onopen={(id) => void navigate(id)}
    onsetup={(id) => void setup(id)}
    onrecover={recover}
    oncreate={createProfile}
    ondemo={() => catalog.announce('Start moneyflow web with --demo for a temporary profile.')}
    onexit={() => catalog.announce('The Moneyflow web server remains available in this tab.')}
  />
{:else if controller?.projection}
  <AppShell {controller} onreconnect={() => profileID && void setup(profileID)} />
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
      {#if controller?.problem?.kind === 'invalid-view'}
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
      {:else if controller?.problem?.kind === 'request' && controller.projection === undefined}
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
      <p class="kit-sr-only" aria-live="polite">{controller?.announcement}</p>
    </main>

    <StatusBar>
      {#snippet left()}
        <span role="status">
          {controller?.loading
            ? 'Loading profile data…'
            : (controller?.projection?.status ?? 'Ready')}
        </span>
      {/snippet}
      {#snippet right()}
        <span>profile · {basePath}</span>
      {/snippet}
    </StatusBar>
  </div>
{/if}
