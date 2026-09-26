<script lang="ts">
  import { Button, DetailDrawer } from '@kenn-io/kit-ui'

  import type { ProviderController } from '../lib/controller/provider'

  interface Props {
    controller: ProviderController
    onreconnect?: (() => void) | undefined
  }

  let { controller, onreconnect }: Props = $props()
  const refreshing = $derived(controller.state.phase === 'refreshing')
  const available = $derived(controller.state.capability?.available === true)
  const progress = $derived(controller.state.status?.progress)

  function confirmationKeydown(event: KeyboardEvent): void {
    if (controller.state.phase !== 'confirmation') return
    if (event.key === 'Enter') {
      event.preventDefault()
      event.stopPropagation()
      void controller.confirm()
    } else if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      controller.dismissConfirmation()
    }
  }
</script>

<svelte:window onkeydown={confirmationKeydown} />

<div class="provider-status">
  {#if controller.state.phase === 'reconnect' && onreconnect}
    <Button size="sm" tone="info" surface="solid" onclick={onreconnect}>Reconnect provider</Button>
  {:else}
    <Button
      size="sm"
      disabled={!available || refreshing}
      title={controller.state.capability?.reason}
      onclick={() => void controller.refresh()}
      >{refreshing ? 'Refreshing…' : 'Refresh provider data'}</Button
    >
  {/if}
  {#if !available && controller.state.capability?.reason}
    <span id="provider-refresh-reason" class="kit-sr-only">
      {controller.state.capability.reason}
    </span>
  {/if}
  {#if progress && (refreshing || progress.total > 0)}
    <span role="status" aria-live="polite">
      Refreshing provider data: {progress.fetched} of {progress.total}
    </span>
  {/if}
</div>

{#if controller.state.phase === 'confirmation' && controller.state.status}
  <DetailDrawer
    title="Confirm provider refresh"
    ariaLabel="Confirm provider refresh"
    onclose={controller.dismissConfirmation}
  >
    <p>
      This refresh would remove
      {controller.state.status.summary.removed_transactions} posted transactions.
    </p>
    <p>Review the count before accepting this provider snapshot.</p>
    <div class="editing-actions">
      <Button onclick={controller.dismissConfirmation}>Cancel</Button>
      <Button tone="danger" surface="solid" onclick={() => void controller.confirm()}>
        Confirm refresh
      </Button>
    </div>
  </DetailDrawer>
{/if}
