<script lang="ts">
  import type { ProviderController } from '../lib/controller/provider'
  import { elapsed, providerLabel, refreshOwner } from '../lib/controller/refresh-notice'

  interface Props {
    controller: ProviderController
    /** The reader's clock, which advances once a second while the dialog is open. */
    now: number
  }

  let { controller, now }: Props = $props()
  const providerState = $derived(controller.state)
  const status = $derived(providerState.status)
  const view = $derived(providerState.dialog?.view)
  const progress = $derived(status?.progress)
  const determinate = $derived(progress !== undefined && progress.total > 0)
  const subject = $derived(
    providerState.kind === 'recent'
      ? 'Recent data (last 90 days)'
      : providerState.kind === 'full'
        ? 'Complete history'
        : undefined,
  )
  const counted = $derived(
    determinate && progress ? `Fetched ${progress.fetched} of ${progress.total}` : undefined,
  )
</script>

<div class="mf-refresh__progress">
  {#if subject}<p class="mf-refresh__subject">{subject}</p>{/if}
  {#if view === 'other'}
    <p>{refreshOwner(status?.owner_renderer)} is refreshing this profile. Cancel it there.</p>
  {/if}
  {#if determinate && progress}
    <progress
      class="mf-refresh__bar"
      aria-label="Refresh progress"
      max={progress.total}
      value={progress.fetched}
    ></progress>
  {:else}
    <progress class="mf-refresh__bar" aria-label="Refresh progress"></progress>
  {/if}
  <p class="mf-refresh__count">
    {view === 'stopping'
      ? 'Stopping…'
      : (counted ?? `Waiting for ${providerLabel(status?.provider_kind)}…`)}
  </p>
  <p class="mf-refresh__meta">
    Elapsed {elapsed(Math.max(0, now - (providerState.dialog?.startedAt ?? now)))}
  </p>
</div>
