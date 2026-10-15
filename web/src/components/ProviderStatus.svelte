<script lang="ts">
  import { Button, formatRefreshAge, IconButton, Tooltip } from '@kenn-io/kit-ui'
  import PlugZapIcon from '@lucide/svelte/icons/plug-zap'
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw'

  import type { ProviderController } from '../lib/controller/provider'
  import { progressText, refreshOwner } from '../lib/controller/refresh-notice'
  import { status } from '../lib/shell/status.svelte'

  interface Props {
    controller: ProviderController
    onreconnect?: (() => void) | undefined
    /** Icon buttons with tooltips for the narrow icon rail. */
    rail?: boolean
  }

  let { controller, onreconnect, rail = false }: Props = $props()
  const refreshing = $derived(controller.state.phase === 'refreshing')
  const available = $derived(controller.state.capability?.available === true)
  const progress = $derived(progressText(controller.state.status?.progress))
  const reconnect = $derived(controller.state.phase === 'reconnect' && onreconnect !== undefined)
  const showProgress = $derived(refreshing && progress !== undefined)
  const subject = $derived(
    controller.state.kind === 'recent'
      ? 'recent data (last 90 days)'
      : controller.state.kind === 'full'
        ? 'full history'
        : 'provider data',
  )
  // Why refresh is unavailable, who else is refreshing, or what the last refresh could not do.
  // The refresh dialog and the stale banner own refresh failures, so they are not repeated here.
  const notice = $derived.by(() => {
    const { capability, status } = controller.state
    if (!available && capability?.reason) return capability.reason
    if (controller.state.dialog?.view === 'other') {
      return `${refreshOwner(status?.owner_renderer)} is refreshing this profile.`
    }
    return status?.warning
  })
  let now = $state(Date.now())
  const updated = $derived.by(() => {
    const lastSuccess = controller.state.status?.last_success
    if (!lastSuccess) return undefined
    const at = Date.parse(lastSuccess)
    return formatRefreshAge(at, Math.max(now, at))
  })
  /** Monarch refreshes the last 90 days by default; R asks for complete history. */
  const historyHint = $derived(
    controller.state.status?.provider_kind === 'monarch' && !refreshing
      ? 'Complete history · R'
      : undefined,
  )
  const refreshTip = $derived(
    [refreshing ? 'Refreshing…' : 'Refresh provider data · r', historyHint, updated, notice]
      .filter(Boolean)
      .join(' · '),
  )

  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 60_000)
    return () => clearInterval(timer)
  })

  /**
   * The rail's Refresh icon is aria-disabled while refresh cannot start, so it keeps focus and its
   * tooltip opens for keyboard users. kit's icon button then swallows a press, so this sees the
   * press first and, when refresh is unavailable, says why in the status line, as `r` does.
   */
  function explainUnavailable(): void {
    const reason = controller.state.capability?.reason
    if (!available && reason) status.notify(reason)
  }
</script>

{#if rail}
  {#if reconnect}
    <Tooltip text="Reconnect provider">
      <IconButton ariaLabel="Reconnect provider" title="" tone="info" onclick={onreconnect}>
        <PlugZapIcon size={14} aria-hidden="true" />
      </IconButton>
    </Tooltip>
  {:else}
    <span onclickcapture={explainUnavailable}>
      <Tooltip text={refreshTip}>
        <IconButton
          ariaLabel="Refresh provider data"
          title=""
          ariaDisabled={!available || refreshing}
          onclick={() => void controller.refresh()}
        >
          <RefreshCwIcon size={14} aria-hidden="true" />
        </IconButton>
      </Tooltip>
    </span>
  {/if}
{:else}
  <div class="provider-status">
    {#if showProgress && progress}
      <span class="provider-status__meta" role="status" aria-live="polite">
        Refreshing {subject} · {progress}
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
        title={controller.state.capability?.reason ?? historyHint}
        ariaLabel={refreshing ? undefined : 'Refresh provider data'}
        onclick={() => void controller.refresh()}
        ><RefreshCwIcon size={12} aria-hidden="true" />{refreshing
          ? 'Refreshing…'
          : 'Refresh'}</Button
      >
    {/if}
    {#if notice}
      <span class="provider-status__meta">{notice}</span>
    {/if}
  </div>
{/if}
