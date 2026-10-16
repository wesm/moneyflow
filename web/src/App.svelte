<script lang="ts">
  import { onMount, tick } from 'svelte'

  import { createMoneyflowClient } from './lib/api/client'
  import { createCatalogClient, type ProfileSummary } from './lib/api/catalog-client'
  import ProfileSelector from './components/profiles/ProfileSelector.svelte'
  import { createCatalogController, type CatalogController } from './lib/controller/catalog.svelte'
  import {
    createViewController,
    type ViewController,
  } from './lib/controller/view-controller.svelte'
  import { profileApplicationPath } from './lib/controller/routing'
  import { historyViewState, type ViewStateAdapter } from './lib/controller/view-state'
  import MoneyflowSetup from './lib/views/MoneyflowSetup.svelte'
  import MoneyflowWorkspace from './lib/views/MoneyflowWorkspace.svelte'
  import { createTokenFetch } from './standalone/token-fetch'

  type SetupProvider = 'monarch' | 'ynab' | 'simplefin' | 'amazon'

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
  const tokenFetch = standaloneFetch()
  const viewState = profileViewState()
  const controller = resolveController()
  const catalog = resolveCatalog()
  let setupFlow = $state<{
    provider: SetupProvider
    profileID: string
    amazonSettings?: { currency: string; scale: number }
  }>()
  /** A profile added in this tab whose setup has not finished; cancelling setup removes it. */
  let createdProfileID: string | undefined
  let closingSetup = false

  /** Standalone moneyflow's server checks mutation tokens; this fetch adds them. */
  function standaloneFetch(): typeof fetch {
    return createTokenFetch(basePath)
  }

  function profileViewState(): ViewStateAdapter | undefined {
    if (!profileID) return undefined
    return historyViewState(profileApplicationPath(basePath, profileID))
  }

  function resolveController(): ViewController | undefined {
    if (suppliedController) return suppliedController
    if (!profileID || !viewState) return undefined
    return createViewController({
      client: createMoneyflowClient(basePath, profileID, tokenFetch),
      viewState,
      autoRefresh: true,
    })
  }

  function resolveCatalog(): CatalogController | undefined {
    if (suppliedCatalog) return suppliedCatalog
    if (profileID || suppliedController) return undefined
    return createCatalogController({ client: createCatalogClient(basePath, tokenFetch) })
  }

  async function canonicalID(
    selector: string,
    providerKind?: 'monarch' | 'ynab' | 'simplefin' | 'amazon' | 'local',
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
    const profile = catalog?.state.profiles.find(
      (candidate) => candidate.id === selector || candidate.key === selector,
    )
    await startSetup(selector, profile?.provider_kind === 'ynab' ? 'ynab' : 'monarch')
  }

  async function startSetup(
    selector: string,
    provider: SetupProvider,
    amazonSettings?: { currency: string; scale: number },
  ): Promise<void> {
    const id = await canonicalID(selector, provider)
    if (!id) return
    // A claimed SimpleFIN connection must not be rolled back by generic catalog cancellation.
    if (createdProfileID !== id || provider === 'simplefin') createdProfileID = undefined
    setupFlow = { provider, profileID: id, ...(amazonSettings ? { amazonSettings } : {}) }
  }

  async function closeSetup(): Promise<void> {
    if (closingSetup) return
    closingSetup = true
    const id = createdProfileID
    createdProfileID = undefined
    if (catalog && id) await catalog.cancelNew(id)
    else if (catalog) await catalog.load()
    setupFlow = undefined
    closingSetup = false
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

  function completeSetup(id: string): void {
    createdProfileID = undefined
    if (setupFlow?.provider === 'amazon') {
      setupFlow = undefined
      // From a profile's workspace, the workspace returns and rechecks its data.
      if (!catalog) return
    }
    void navigate(id)
  }

  async function createProfile(
    name: string,
    provider: 'monarch' | 'ynab' | 'simplefin' | 'amazon' | 'local',
  ): Promise<ProfileSummary | undefined> {
    if (!catalog) return undefined
    const created = await catalog.create(name, provider)
    if ((provider === 'monarch' || provider === 'ynab' || provider === 'amazon') && created?.id) {
      createdProfileID = created.id
    }
    return created
  }

  function reconnectProvider(current: ViewController): SetupProvider {
    const kind = current.projection?.profile_kind
    return kind === 'ynab' || kind === 'simplefin' ? kind : 'monarch'
  }

  /** The profile's display name for the load-failure page, once the catalog gives it. */
  let profileName = $state<string | undefined>()
  let profileNameRequested = false
  const loadFailed = $derived(
    controller?.problem !== undefined &&
      controller.problem.kind !== 'invalid-view' &&
      !controller.projection,
  )
  $effect(() => {
    if (!loadFailed || !profileID || profileNameRequested) return
    profileNameRequested = true
    void nameProfile(profileID)
  })

  /**
   * Looks up the profile's display name in the catalog. The page has only the profile's ID, and
   * the view that failed carries no name. Without an answer, the page keeps "this profile": the
   * reason the load failed is already on the page, and the name only labels it.
   */
  async function nameProfile(id: string): Promise<void> {
    try {
      const { profiles } = await createCatalogClient(basePath, tokenFetch).list()
      const profile = profiles?.find((entry) => entry.id === id || entry.key === id)
      profileName = profile?.display_name || undefined
    } catch {
      profileName = undefined
    }
  }

  async function recover(id: string, confirmed: boolean): Promise<void> {
    await catalog?.recovery(id, confirmed)
  }

  onMount(() => {
    if (catalog) void catalog.load()
  })
</script>

<div class="mf-views">
  {#if setupFlow}
    {#key setupFlow}
      <MoneyflowSetup
        apiBase={basePath}
        fetch={tokenFetch}
        provider={setupFlow.provider}
        profileID={setupFlow.profileID}
        amazonSettings={setupFlow.amazonSettings}
        oncomplete={completeSetup}
        oncancel={() => void closeSetup()}
      />
    {/key}
  {:else if catalog}
    <ProfileSelector
      profiles={catalog.state.profiles}
      loading={catalog.state.loading}
      announcement={catalog.state.announcement}
      problem={catalog.state.problem}
      recovery={catalog.state.recovery}
      onopen={(id) => void navigate(id)}
      onsetup={(id) => void setup(id)}
      onsimplefinsetup={(id) => void startSetup(id, 'simplefin')}
      onamazonsetup={(id) => void startSetup(id, 'amazon')}
      onrecover={recover}
      oncreate={createProfile}
      ondemo={() => globalThis.location.assign(`${basePath}demo/`)}
      onexit={() => catalog.announce('The Moneyflow web server remains available in this tab.')}
    />
  {:else if controller && profileID && viewState}
    <MoneyflowWorkspace
      {controller}
      apiBase={basePath}
      fetch={tokenFetch}
      {profileID}
      {profileName}
      {viewState}
      chrome
      onreconnect={() => void startSetup(profileID, reconnectProvider(controller))}
      onamazonimport={(settings) => void startSetup(profileID, 'amazon', settings)}
    />
  {/if}
</div>
