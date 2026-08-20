<script lang="ts" module>
  function statusLabel(status: string): string {
    const labels: Record<string, string> = {
      ready: 'Ready',
      reconnect: 'Reconnect',
      setup_incomplete: 'Setup incomplete',
      local_only: 'Local only',
      needs_recovery: 'Needs recovery',
      requires_newer_moneyflow: 'Requires newer Moneyflow',
      manifest_unsupported: 'Unsupported manifest',
    }
    return labels[status] ?? 'Unavailable'
  }

  function providerLabel(provider: string): string {
    return provider === 'monarch'
      ? 'Monarch Money'
      : provider === 'amazon'
        ? 'Amazon orders'
        : 'Local profile'
  }

  function inputTarget(target: EventTarget | null): boolean {
    return target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement
  }
</script>

<script lang="ts">
  import { Button, Card, StatusBar, ThemeToggle, TopBar } from '@kenn-io/kit-ui'
  import { onMount, tick } from 'svelte'

  import type { ProfileSummary, RecoveryResponse } from '../../lib/api/catalog-client'
  import ProfileNameForm from './ProfileNameForm.svelte'
  import ProviderSelector from './ProviderSelector.svelte'
  import RecoveryPanel from './RecoveryPanel.svelte'

  interface Props {
    profiles: ProfileSummary[]
    loading: boolean
    announcement: string
    problem?: string | undefined
    recovery?: RecoveryResponse | undefined
    onopen: (profileID: string) => void
    onsetup: (profileID: string) => void
    onamazonsetup?: (profileID: string) => void
    onrecover: (profileID: string, confirmed: boolean) => Promise<void> | void
    oncreate: (
      name: string,
      provider: 'monarch' | 'amazon' | 'local',
    ) => Promise<ProfileSummary | undefined> | ProfileSummary | undefined
    ondemo: () => void
    onexit: () => void
  }

  let {
    profiles,
    loading,
    announcement,
    problem,
    recovery,
    onopen,
    onsetup,
    onamazonsetup,
    onrecover,
    oncreate,
    ondemo,
    onexit,
  }: Props = $props()
  type View = 'list' | 'provider' | 'name' | 'recovery' | 'guidance' | 'local'
  let view = $state<View>('list')
  let active = $state(0)
  let selected = $state<ProfileSummary | undefined>()
  let provider = $state<'monarch' | 'amazon' | 'local'>('monarch')
  let profileList = $state<HTMLElement | undefined>()
  let wasLoading = $state(false)
  const entries = $derived([
    ...profiles.map((profile) => ({ kind: 'profile' as const, profile })),
    { kind: 'demo' as const },
    { kind: 'add' as const },
  ])

  function keydown(event: KeyboardEvent): void {
    if (event.defaultPrevented || inputTarget(event.target)) return
    if (view !== 'list') {
      if (event.key === 'Escape') {
        event.preventDefault()
        back()
      }
      return
    }
    if (event.key === 'a' || event.key === 'n') {
      event.preventDefault()
      view = 'provider'
      return
    }
    if (event.key === 'd') {
      event.preventDefault()
      ondemo()
      return
    }
    if (event.key === 'Escape' || event.key === 'q') {
      event.preventDefault()
      onexit()
      return
    }
    if (event.key === 'ArrowDown' || event.key === 'j') active = (active + 1) % entries.length
    else if (event.key === 'ArrowUp' || event.key === 'k')
      active = (active - 1 + entries.length) % entries.length
    else if (event.key === 'Home') active = 0
    else if (event.key === 'Enter') return choose(active)
    else return
    event.preventDefault()
    void focusActive()
  }

  function choose(index: number): void {
    active = index
    const entry = entries[index]
    if (!entry) return
    if (entry.kind === 'demo') return ondemo()
    if (entry.kind === 'add') {
      view = 'provider'
      return
    }
    selectProfile(entry.profile)
  }

  function selectProfile(profile: ProfileSummary): void {
    selected = profile
    if (profile.status === 'ready') {
      onopen(profile.id ?? profile.key)
      return
    }
    if (profile.status === 'reconnect' || profile.status === 'setup_incomplete') {
      if (profile.provider_kind === 'amazon') (onamazonsetup ?? onsetup)(profile.id ?? profile.key)
      else onsetup(profile.id ?? profile.key)
      return
    }
    if (profile.status === 'local_only') {
      view = 'local'
      return
    }
    if (profile.status === 'needs_recovery') {
      view = 'recovery'
      if (profile.id) void onrecover(profile.id, false)
      return
    }
    view = 'guidance'
  }

  function back(): void {
    view = view === 'name' ? 'provider' : 'list'
    selected = undefined
    void focusActive()
  }

  async function create(name: string): Promise<void> {
    const created = await oncreate(name, provider)
    if (!created?.id) return
    if (provider === 'local') onopen(created.id)
    else if (provider === 'amazon') (onamazonsetup ?? onsetup)(created.id)
    else onsetup(created.id)
  }

  async function focusActive(): Promise<void> {
    await tick()
    profileList?.querySelectorAll<HTMLButtonElement>('button').item(active).focus()
  }

  $effect(() => {
    const count = entries.length
    if (active >= count) active = Math.max(0, count - 1)
    if (wasLoading && !loading) {
      active = 0
      void focusActive()
    }
    wasLoading = loading
  })

  onMount(() => {
    if (!loading) void focusActive()
  })
</script>

<svelte:window onkeydown={keydown} />

<div class="moneyflow-app profile-selector">
  <TopBar ariaLabel="Moneyflow profiles">
    {#snippet left()}<span class="moneyflow-brand">Moneyflow</span>{/snippet}
    {#snippet right()}<ThemeToggle size="sm" />{/snippet}
  </TopBar>
  <main class="profile-main" aria-label="Moneyflow profile selection">
    {#if view === 'provider'}
      <ProviderSelector
        onselect={(choice) => {
          provider = choice
          view = 'name'
        }}
        onback={back}
      />
    {:else if view === 'name'}
      <ProfileNameForm {provider} onsubmit={(name) => void create(name)} onback={back} />
    {:else if (view === 'recovery' || view === 'guidance') && selected}
      <RecoveryPanel
        profile={selected}
        {recovery}
        busy={loading}
        onconfirm={() => selected?.id && void onrecover(selected.id, true)}
        onback={back}
      />
    {:else if view === 'local' && selected}
      <section class="profile-panel" aria-labelledby="offline-title">
        <p class="moneyflow-eyebrow">{selected.display_name}</p>
        <h1 id="offline-title">Open this profile offline?</h1>
        <p>Local financial data is available, but this profile is not connected to a provider.</p>
        <div class="profile-actions">
          <Button onclick={back}>Back</Button>
          <Button tone="info" surface="solid" onclick={() => selected?.id && onopen(selected.id)}>
            Open Offline
          </Button>
        </div>
      </section>
    {:else}
      <section class="profile-panel" aria-labelledby="profiles-title">
        <p class="moneyflow-eyebrow">Profiles</p>
        <h1 id="profiles-title">Choose a Moneyflow profile</h1>
        <p>Open an existing profile, use synthetic demo data, or connect another profile.</p>
        {#if problem}<p class="editing-error" role="alert">{problem}</p>{/if}
        <div class="profile-list" role="list" bind:this={profileList}>
          {#each entries as entry, index (entry.kind === 'profile' ? entry.profile.key : entry.kind)}
            {#if entry.kind === 'profile'}
              <Card
                title={entry.profile.display_name}
                eyebrow={providerLabel(entry.profile.provider_kind)}
                meta={statusLabel(entry.profile.status)}
                selected={active === index}
                onclick={() => choose(index)}
              />
            {:else if entry.kind === 'demo'}
              <Card
                title="Demo"
                eyebrow="Synthetic data"
                meta="Temporary"
                selected={active === index}
                onclick={() => choose(index)}
              />
            {:else}
              <Card
                title="Add profile"
                eyebrow="Provider or local"
                meta="New"
                selected={active === index}
                onclick={() => choose(index)}
              />
            {/if}
          {/each}
        </div>
      </section>
    {/if}
    <p class="kit-sr-only" aria-live="polite">{announcement}</p>
  </main>
  <StatusBar>
    {#snippet left()}<span role={view === 'list' ? 'status' : undefined}
        >{loading ? 'Loading profiles…' : announcement}</span
      >{/snippet}
    {#snippet right()}<span>↑/↓ or j/k · Enter · a/n Add · d Demo · q Exit</span>{/snippet}
  </StatusBar>
</div>
