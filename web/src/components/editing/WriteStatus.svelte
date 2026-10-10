<script lang="ts">
  import { Button } from '@kenn-io/kit-ui'

  import type { ProviderWriteController } from '../../lib/controller/provider-write'

  interface Props {
    controller: ProviderWriteController
    providerName: string
    onreconnect?: (() => void) | undefined
  }

  let { controller, providerName, onreconnect }: Props = $props()
  const status = $derived(controller.state.status)
  const done = $derived(status && status.total > 0 ? (100 * status.completed) / status.total : 0)
  /** When a rate-limited write may resume, in the reader's locale. */
  const eligible = $derived.by(() => {
    if (status?.phase !== 'rate_limited' || !status.next_eligible) return undefined
    const time = new Date(status.next_eligible)
    return Number.isNaN(time.getTime()) ? status.next_eligible : time.toLocaleString()
  })
</script>

<p class="mf-write__announcement" role="status">{controller.state.announcement}</p>
{#if status}
  <div class="mf-write__bar" aria-hidden="true"><span style:width={`${done}%`}></span></div>
  <dl class="mf-write__stats">
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
    {#if eligible}<div>
        <dt>Next eligible attempt</dt>
        <dd><time datetime={status.next_eligible}>{eligible}</time></dd>
      </div>{/if}
  </dl>
  {#if controller.can('pause')}
    <p class="mf-write__note">
      Pausing stops future provider calls. Changes already accepted by {providerName} cannot be cancelled.
    </p>
  {/if}
  {#if controller.can('reconcile')}
    <p class="mf-write__note">
      Stop and reconcile discards remaining edits and reloads provider data.
    </p>
  {/if}
  <div class="mf-write__actions">
    {#if controller.can('pause')}<Button onclick={() => void controller.pause()}>Pause</Button>{/if}
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

<style>
  .mf-write__announcement {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: var(--font-weight-semibold);
  }
  .mf-write__bar {
    height: 6px;
    overflow: hidden;
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
  }
  .mf-write__bar span {
    display: block;
    height: 100%;
    background: var(--mf-pending);
  }
  .mf-write__stats {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-5) var(--space-8);
    margin: 0;
  }
  .mf-write__stats dt {
    color: var(--text-secondary);
    font-size: var(--font-size-sm);
  }
  .mf-write__stats dd {
    margin: 0;
    font-size: var(--font-size-lg);
    font-variant-numeric: tabular-nums;
  }
  .mf-write__note {
    margin: 0;
    color: var(--text-secondary);
  }
  .mf-write__actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-4);
  }
</style>
