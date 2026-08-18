<script lang="ts">
  import { Button, DetailDrawer } from '@kenn-io/kit-ui'

  import type { ProviderWriteController } from '../../lib/controller/provider-write'

  interface Props {
    controller: ProviderWriteController
    onclose: () => void
    onreconnect?: (() => void) | undefined
  }

  let { controller, onclose, onreconnect }: Props = $props()
  const status = $derived(controller.state.status)
</script>

<DetailDrawer
  title="Monarch write status"
  ariaLabel="Monarch write status"
  {onclose}
  width="min(560px, 100vw)"
>
  <p role="status">{controller.state.announcement}</p>
  {#if status}
    <dl class="write-status">
      <div>
        <dt>Progress</dt>
        <dd>{status.completed} of {status.total} complete</dd>
      </div>
      <div>
        <dt>Remaining</dt>
        <dd>{status.remaining}</dd>
      </div>
      {#if status.failed}<div>
          <dt>Failed</dt>
          <dd>{status.failed}</dd>
        </div>{/if}
      {#if status.overrides}<div>
          <dt>Provider overrides</dt>
          <dd>{status.overrides}</dd>
        </div>{/if}
    </dl>
    <p>
      Pausing stops future provider calls. Changes already accepted by Monarch cannot be cancelled.
    </p>
    <div class="editing-actions">
      <Button onclick={onclose}>Close</Button>
      {#if controller.can('pause')}<Button onclick={() => void controller.pause()}>Pause</Button
        >{/if}
      {#if controller.can('resume')}<Button
          tone="info"
          surface="solid"
          onclick={() => void controller.resume()}>Resume</Button
        >{/if}
      {#if controller.can('reconcile')}<Button
          tone="danger"
          onclick={() => void controller.reconcile()}>Stop and reconcile</Button
        >{/if}
      {#if controller.can('confirm')}<Button
          tone="danger"
          surface="solid"
          onclick={() => void controller.confirm()}>Confirm reconciliation</Button
        >{/if}
      {#if controller.can('reconnect') && onreconnect}<Button
          tone="info"
          surface="solid"
          onclick={onreconnect}>Reconnect Monarch</Button
        >{/if}
    </div>
  {/if}
</DetailDrawer>
