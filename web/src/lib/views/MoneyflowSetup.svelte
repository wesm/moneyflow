<script lang="ts">
  import { Button } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'

  import AmazonImportWizard from '../../components/profiles/AmazonImportWizard.svelte'
  import OnboardingWizard from '../../components/profiles/OnboardingWizard.svelte'
  import SimpleFINOnboardingWizard from '../../components/profiles/SimpleFINOnboardingWizard.svelte'
  import { createCatalogClient } from '../api/catalog-client'
  import { MoneyflowProblem } from '../api/client'
  import {
    createAmazonImportController,
    createAmazonImportTransport,
    type AmazonImportController,
  } from '../controller/amazon-import.svelte'
  import {
    createOnboardingController,
    createOnboardingTransport,
    type OnboardingController,
  } from '../controller/onboarding.svelte'
  import {
    createSimpleFINOnboardingController,
    createSimpleFINOnboardingTransport,
    type SimpleFINOnboardingController,
  } from '../controller/simplefin-onboarding.svelte'
  import type { ProviderKind } from './views'

  interface SharedProps {
    /** Where the host mounts moneyflow's API, such as '/embedded/moneyflow/'. */
    apiBase: string
    /** The host's fetch; it carries whatever the host's API requires. */
    fetch: typeof fetch
    provider: ProviderKind
    /** Starts an Amazon import with these settings instead of asking for them. */
    amazonSettings?: { currency: string; scale: number } | undefined
    /**
     * Frames setup as a page of its own, with moneyflow's top bar, theme button and status bar.
     * A host leaves it off and frames setup itself, for example in a dialog.
     */
    chrome?: boolean
    oncomplete: (profileID: string) => void
    oncancel: () => void
  }

  /** Setup reconnects or unlocks an existing profile, or creates one under the host's name. */
  type Props = SharedProps &
    (
      | {
          /** The profile to reconnect or unlock. It keeps its name. */
          profileID: string
          name?: undefined
        }
      | {
          profileID?: undefined
          /** The display name of the profile that setup creates. */
          name: string
        }
    )

  const {
    apiBase,
    fetch: hostFetch,
    provider,
    profileID,
    name,
    amazonSettings,
    chrome = false,
    oncomplete,
    oncancel,
  }: Props = $props()

  type Flow =
    | { kind: 'starting' }
    | { kind: 'failed'; message: string }
    | { kind: 'onboarding'; id: string; controller: OnboardingController }
    | { kind: 'simplefin'; id: string; controller: SimpleFINOnboardingController }
    | { kind: 'amazon'; id: string; controller: AmazonImportController }

  let flow = $state<Flow>({ kind: 'starting' })
  /** The profile this setup created, which cancelling removes again. */
  let created: string | undefined
  let closed = false

  async function start(): Promise<void> {
    const id = profileID === undefined ? await create(name) : profileID
    if (id === undefined || closed) return
    if (provider === 'local' || provider === 'csv') {
      complete(id)
    } else if (provider === 'simplefin') {
      // Setup claims the SimpleFIN connection, which must stay with its profile.
      created = undefined
      const transport = createSimpleFINOnboardingTransport(apiBase, id, hostFetch)
      const controller = createSimpleFINOnboardingController({ profileID: id, transport })
      flow = { kind: 'simplefin', id, controller }
    } else if (provider === 'amazon') {
      const transport = createAmazonImportTransport(apiBase, id, hostFetch)
      const controller = createAmazonImportController({ transport })
      flow = { kind: 'amazon', id, controller }
      if (amazonSettings) await controller.start(amazonSettings.currency, amazonSettings.scale)
    } else {
      const transport = createOnboardingTransport(apiBase, id, hostFetch)
      const controller = createOnboardingController({ profileID: id, transport })
      flow = { kind: 'onboarding', id, controller }
    }
  }

  async function create(displayName: string): Promise<string | undefined> {
    if (provider === 'csv') {
      flow = { kind: 'failed', message: 'A CSV profile cannot be created here.' }
      return undefined
    }
    try {
      const profile = await createCatalogClient(apiBase, hostFetch).create(displayName, provider)
      created = profile.id
      return profile.id
    } catch (error) {
      const message =
        error instanceof MoneyflowProblem ? error.message : 'The profile could not be created.'
      flow = { kind: 'failed', message }
      return undefined
    }
  }

  function stop(): void {
    if (flow.kind === 'onboarding' || flow.kind === 'simplefin' || flow.kind === 'amazon') {
      flow.controller.destroy()
    }
  }

  function complete(id: string): void {
    stop()
    created = undefined
    oncomplete(id)
  }

  async function cancel(): Promise<void> {
    stop()
    const id = created
    created = undefined
    if (id && !(await removeCreated(id))) {
      flow = {
        kind: 'failed',
        message: 'The new profile could not be removed. It stays in the profile list.',
      }
      return
    }
    oncancel()
  }

  async function removeCreated(id: string): Promise<boolean> {
    try {
      return await createCatalogClient(apiBase, hostFetch).cancelNew(id)
    } catch {
      return false
    }
  }

  onMount(() => {
    void start()
    return () => {
      closed = true
      stop()
    }
  })
</script>

<div class="mf-views">
  {#if flow.kind === 'onboarding'}
    {@const id = flow.id}
    <OnboardingWizard
      controller={flow.controller}
      {chrome}
      oncomplete={() => complete(id)}
      oncancel={() => void cancel()}
      onoffline={() => complete(id)}
    />
  {:else if flow.kind === 'simplefin'}
    {@const id = flow.id}
    <SimpleFINOnboardingWizard
      controller={flow.controller}
      {chrome}
      oncomplete={() => complete(id)}
      oncancel={() => void cancel()}
      onoffline={() => complete(id)}
    />
  {:else if flow.kind === 'amazon'}
    {@const id = flow.id}
    <AmazonImportWizard
      controller={flow.controller}
      {chrome}
      initialCurrency={amazonSettings?.currency ?? 'USD'}
      initialScale={amazonSettings?.scale ?? 2}
      oncomplete={() => complete(id)}
      oncancel={() => void cancel()}
    />
  {:else if flow.kind === 'failed'}
    <section class="moneyflow-message" aria-labelledby="setup-failed-title">
      <p class="moneyflow-eyebrow">Setup</p>
      <h1 id="setup-failed-title">Setup did not finish</h1>
      <p>{flow.message}</p>
      <Button onclick={oncancel}>Back</Button>
    </section>
  {:else}
    <section class="moneyflow-message" aria-labelledby="setup-loading-title">
      <p class="moneyflow-eyebrow">Setup</p>
      <h1 id="setup-loading-title">Starting setup…</h1>
    </section>
  {/if}
</div>
