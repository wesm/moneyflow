<script lang="ts">
  import { Button, DetailDrawer } from '@kenn-io/kit-ui'

  import type { ProviderWriteController } from '../../lib/controller/provider-write'

  interface Props {
    controller: ProviderWriteController
    providerName: string
    onclose: () => void
    onreconnect?: (() => void) | undefined
  }

  let { controller, providerName, onclose, onreconnect }: Props = $props()
  const status = $derived(controller.state.status)
</script>

<DetailDrawer
  title={`${providerName} write status`}
  ariaLabel={`${providerName} write status`}
  {onclose}
  width="min(560px, 100vw)"
>
  <div class="write-summary">
    <p role="status">{controller.state.announcement}</p>
    {#if status}
      {#if status.phase || status.completed_at}
        <dl class="write-status">
          <div>
            <dt>Progress</dt>
            <dd>{status.completed} of {status.total} complete</dd>
          </div>
          {#if status.phase}<div>
              <dt>Remaining</dt>
              <dd>{status.remaining}</dd>
            </div>{/if}
          {#if status.failed}<div>
              <dt>Failed</dt>
              <dd>{status.failed}</dd>
            </div>{/if}
          {#if status.overrides}<div>
              <dt>Provider overrides</dt>
              <dd>{status.overrides}</dd>
            </div>{/if}
          {#if status.phase === 'rate_limited' && status.next_eligible}<div>
              <dt>Next eligible attempt</dt>
              <dd><time datetime={status.next_eligible}>{status.next_eligible}</time></dd>
            </div>{/if}
          {#if status.completed_at}<div>
              <dt>Finished</dt>
              <dd>
                <time datetime={status.completed_at}
                  >{new Date(status.completed_at).toLocaleString()}</time
                >
              </dd>
            </div>{/if}
        </dl>
      {/if}
      {#if controller.can('pause')}
        <p>
          Pausing stops future provider calls. Changes already accepted by {providerName} cannot be cancelled.
        </p>
      {/if}
      {#if controller.can('reconcile')}
        <p>Stop and reconcile discards remaining edits and reloads provider data.</p>
      {/if}
      <div class="write-actions">
        <Button onclick={onclose}>Close</Button>
        {#if controller.can('pause')}<Button onclick={() => void controller.pause()}>Pause</Button
          >{/if}
        {#if controller.can('resume')}<Button
            tone="info"
            surface="solid"
            onclick={() => void controller.resume()}
            >{status.reason === 'provider_write_outcome_unknown'
              ? 'Check and resume'
              : 'Resume'}</Button
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
            onclick={onreconnect}>Reconnect {providerName}</Button
          >{/if}
      </div>
    {/if}
  </div>
</DetailDrawer>

<style>
  .write-summary {
    display: grid;
    gap: var(--space-5);
    padding: var(--space-5);
  }
  .write-summary p {
    margin: 0;
  }
  .write-status {
    display: grid;
    gap: var(--space-3);
    margin: 0;
  }
  .write-status > div {
    display: flex;
    justify-content: space-between;
    gap: var(--space-4);
  }
  dt {
    color: var(--text-secondary);
  }
  dd {
    margin: 0;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .write-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--space-2);
  }
</style>
