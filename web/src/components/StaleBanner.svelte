<script lang="ts">
  import { Button, Card } from '@kenn-io/kit-ui'

  import type { ProviderController } from '../lib/controller/provider'
  import { staleNotice } from '../lib/controller/refresh-notice'

  interface Props {
    controller: ProviderController
    /** Opens the provider's reconnect flow. */
    onreconnect?: (() => void) | undefined
  }

  let { controller, onreconnect }: Props = $props()
  let now = $state(Date.now())
  const notice = $derived(staleNotice(controller.state, now))
  const capability = $derived(controller.state.capability)

  // The age and the end of the retry delay move with the clock.
  $effect(() => {
    const clock = setInterval(() => (now = Date.now()), 15_000)
    return () => clearInterval(clock)
  })
</script>

{#if notice}
  <div class="mf-stale-banner">
    <Card level="inset" padding="sm" eyebrow="Not refreshed" eyebrowTone="warning">
      <div class="mf-lock-banner__row">
        <p class="mf-lock-banner__text" role="status">{notice.text}</p>
        <div class="mf-stale-banner__actions">
          {#if notice.checkFailed}
            <Button size="sm" onclick={() => void controller.poll()}>Retry</Button>
          {:else if notice.reconnect && onreconnect}
            <Button size="sm" tone="info" surface="solid" onclick={onreconnect}>Reconnect</Button>
          {:else}
            {#if notice.retryAt}
              <span class="mf-stale-banner__wait">
                {notice.manualWaits ? 'Available' : 'Retry available'} at {notice.retryAt}
              </span>
            {/if}
            <Button
              size="sm"
              disabled={(notice.manualWaits && notice.retryAt !== undefined) ||
                capability?.available !== true}
              title={capability?.reason}
              onclick={() => void controller.refresh()}>Refresh</Button
            >
          {/if}
        </div>
      </div>
    </Card>
  </div>
{/if}
