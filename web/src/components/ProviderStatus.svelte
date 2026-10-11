<script lang="ts">
  import { Button, formatRefreshAge, IconButton, Tooltip } from '@kenn-io/kit-ui'
  import PlugZapIcon from '@lucide/svelte/icons/plug-zap'
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw'

  import type { ProviderController } from '../lib/controller/provider'

  interface Props {
    controller: ProviderController
    onreconnect?: (() => void) | undefined
    /** Icon buttons with tooltips for the narrow icon rail. */
    rail?: boolean
  }

  let { controller, onreconnect, rail = false }: Props = $props()
  const refreshing = $derived(controller.state.phase === 'refreshing')
  const available = $derived(controller.state.capability?.available === true)
  const progress = $derived(controller.state.status?.progress)
  const reconnect = $derived(controller.state.phase === 'reconnect' && onreconnect !== undefined)
  const showProgress = $derived(progress !== undefined && (refreshing || progress.total > 0))
  const subject = $derived(
    controller.state.kind === 'recent'
      ? 'recent data (last 90 days)'
      : controller.state.kind === 'full'
        ? 'full history'
        : 'provider data',
  )
  // Why refresh is unavailable, who else is refreshing, or why the last refresh failed.
  const notice = $derived.by(() => {
    const { announcement, capability, phase, status } = controller.state
    if (!available && capability?.reason) return capability.reason
    if (phase === 'refreshing' && status?.code === 'provider_refresh_in_progress') {
      return status.code_detail
    }
    return phase === 'failed' && announcement ? announcement : undefined
  })
  let now = $state(Date.now())
  const updated = $derived.by(() => {
    const lastSuccess = controller.state.status?.last_success
    if (!lastSuccess) return undefined
    const at = Date.parse(lastSuccess)
    return formatRefreshAge(at, Math.max(now, at))
  })
  const refreshTip = $derived(
    [refreshing ? 'Refreshing…' : 'Refresh provider data · r', updated, notice]
      .filter(Boolean)
      .join(' · '),
  )

  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 60_000)
    return () => clearInterval(timer)
  })
</script>

{#if rail}
  {#if reconnect}
    <Tooltip text="Reconnect provider">
      <IconButton ariaLabel="Reconnect provider" title="" tone="info" onclick={onreconnect}>
        <PlugZapIcon size={14} aria-hidden="true" />
      </IconButton>
    </Tooltip>
  {:else}
    <Tooltip text={refreshTip}>
      <IconButton
        ariaLabel="Refresh provider data"
        title=""
        disabled={!available || refreshing}
        onclick={() => void controller.refresh()}
      >
        <RefreshCwIcon size={14} aria-hidden="true" />
      </IconButton>
    </Tooltip>
  {/if}
{:else}
  <div class="provider-status">
    {#if showProgress && progress}
      <span class="provider-status__meta" role="status" aria-live="polite">
        Refreshing {subject}: {progress.fetched} of {progress.total}
      </span>
    {:else if refreshing && controller.state.kind}
      <span class="provider-status__meta">Refreshing {subject}…</span>
    {:else if updated}
      <span class="provider-status__meta">{updated}</span>
    {/if}
    {#if reconnect}
      <Button
        size="sm"
        tone="info"
        surface="solid"
        ariaLabel="Reconnect provider"
        onclick={onreconnect}>Reconnect</Button
      >
    {:else}
      <Button
        size="sm"
        disabled={!available || refreshing}
        title={controller.state.capability?.reason}
        ariaLabel={refreshing ? undefined : 'Refresh provider data'}
        onclick={() => void controller.refresh()}
        ><RefreshCwIcon size={12} aria-hidden="true" />{refreshing
          ? 'Refreshing…'
          : 'Refresh'}</Button
      >
    {/if}
    {#if notice}
      <span
        class="provider-status__meta"
        class:provider-status__failure={controller.state.phase === 'failed'}>{notice}</span
      >
    {/if}
  </div>
{/if}
