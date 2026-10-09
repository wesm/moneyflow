<script lang="ts">
  import { Button, Card, KbdBadge } from '@kenn-io/kit-ui'

  import type { PendingSummary } from '../../lib/api/client'
  import { pendingParts } from '../../lib/shell/pending'

  interface Props {
    pending: PendingSummary
    reviewAvailable: boolean
    write?: { completed: number; total: number } | undefined
    onreview: () => void
    onwrite: () => void
  }

  let { pending, reviewAvailable, write, onreview, onwrite }: Props = $props()
  const parts = $derived(pendingParts(pending))
</script>

<Card level="inset" padding="sm" class="mf-pending-card">
  <p class="mf-pending-card__text" role="status" aria-label="Pending profile changes">
    <!-- Each phrase stays on one line, so the narrow card wraps only between phrases. -->
    {#each parts as part, index (index)}{index > 0 ? ' ' : ''}<span class="mf-pending-card__part"
        >{part}{index < parts.length - 1 ? ' ·' : ''}</span
      >{/each}
  </p>
  <div class="mf-pending-card__actions">
    <Button
      size="sm"
      ariaLabel="Review pending changes"
      disabled={!reviewAvailable}
      onclick={onreview}>Review{#snippet trailing()}<KbdBadge keys={['w']} />{/snippet}</Button
    >
    {#if write}
      <Button size="sm" onclick={onwrite}>Write {write.completed}/{write.total}</Button>
    {/if}
  </div>
</Card>
