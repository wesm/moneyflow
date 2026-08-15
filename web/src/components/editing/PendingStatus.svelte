<script lang="ts">
  import type { PendingSummary } from '../../lib/api/client'
  interface Props {
    pending: PendingSummary
  }
  let { pending }: Props = $props()
  const text = $derived(
    `${pending.active_operations} pending · ${pending.affected_transactions} transactions${pending.inactive_operations > 0 ? ` · ${pending.inactive_operations} redo` : ''}`,
  )
</script>

<span class="pending-status" role="status" aria-label="Pending profile changes">
  {text}
</span>
