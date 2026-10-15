<script lang="ts">
  import { Button, Card, KbdBadge } from '@kenn-io/kit-ui'

  import type { PendingSummary } from '../../lib/api/client'
  import { focusClicked } from '../../lib/shell/focus'
  import { pendingParts } from '../../lib/shell/pending'

  interface Props {
    pending: PendingSummary
    reviewAvailable: boolean
    write?: { completed: number; total: number } | undefined
    /** The last write finished and its details can still be shown. */
    lastWrite?: boolean
    onreview: () => void
    onwrite: () => void
  }

  let { pending, reviewAvailable, write, lastWrite = false, onreview, onwrite }: Props = $props()
  // The hidden separator keeps the text one phrase list for screen readers and text search.
  const separator = ' · '
  const parts = $derived(pendingParts(pending))
  const waiting = $derived(pending.active_operations > 0)
  /** New pending edits take the card's action from the last write. */
  const showsLastWrite = $derived(lastWrite && !waiting && !write)
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
  {#if waiting || write || showsLastWrite}
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
      {:else if showsLastWrite}
        <Button
          size="sm"
          onclick={(event) => {
            focusClicked(event)
            onwrite()
          }}>Last write</Button
        >
      {/if}
    </div>
  {/if}
</Card>
