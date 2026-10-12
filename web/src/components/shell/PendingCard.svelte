<script lang="ts">
  import { Button, Card, KbdBadge } from '@kenn-io/kit-ui'

  import type { PendingSummary } from '../../lib/api/client'
  import { focusClicked } from '../../lib/shell/focus'
  import { pendingParts } from '../../lib/shell/pending'

  interface Props {
    pending: PendingSummary
    reviewAvailable: boolean
    write?: { completed: number; total: number } | undefined
    onreview: () => void
    onwrite: () => void
  }

  let { pending, reviewAvailable, write, onreview, onwrite }: Props = $props()
  // The hidden separator keeps the text one phrase list for screen readers and text search.
  const separator = ' · '
  const parts = $derived(pendingParts(pending))
  const waiting = $derived(pending.active_operations > 0)
</script>

<Card level="inset" padding="sm" class="mf-pending-card">
  <p
    class="mf-pending-card__text"
    class:mf-pending-card__text--waiting={waiting}
    role="status"
    aria-label="Pending profile changes"
  >
    <span class="mf-pending-card__title">{parts[0]}</span><span class="kit-sr-only"
      >{separator}</span
    ><span class="mf-pending-card__detail">{parts.slice(1).join(separator)}</span>
  </p>
  {#if waiting || write}
    <div class="mf-pending-card__actions">
      {#if waiting}
        <Button
          size="sm"
          ariaLabel="Review pending changes"
          disabled={!reviewAvailable}
          onclick={(event) => {
            focusClicked(event)
            onreview()
          }}>Review{#snippet trailing()}<KbdBadge keys={['w']} />{/snippet}</Button
        >
      {/if}
      {#if write}
        <Button
          size="sm"
          onclick={(event) => {
            focusClicked(event)
            onwrite()
          }}>Write {write.completed}/{write.total}</Button
        >
      {/if}
    </div>
  {/if}
</Card>
