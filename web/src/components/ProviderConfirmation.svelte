<script lang="ts">
  import { Button, DetailDrawer } from '@kenn-io/kit-ui'

  import type { ProviderController } from '../lib/controller/provider'

  interface Props {
    controller: ProviderController
  }

  let { controller }: Props = $props()

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
