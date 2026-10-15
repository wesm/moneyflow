<script lang="ts">
  import { Button, KbdBadge, StatusBar, StatusDot } from '@kenn-io/kit-ui'

  import type { KeyHint } from '../../lib/shell/key-hints'
  import { status, type Connection } from '../../lib/shell/status.svelte'

  /** Requests shorter than this never flash "Updating…". */
  const updatingDelayMs = 300

  interface Props {
    hints: KeyHint[]
    connection: Connection
    revision: string
    /** Pending text, given when the sidebar card is not showing it. */
    pending?: string | undefined
    /**
     * Phone width: the pending text takes the hint area, which has no keyboard to hint at, and
     * the profile revision is left out so the pending text fits.
     */
    compact?: boolean
    onretry: () => void
  }

  let { hints, connection, revision, pending, compact = false, onretry }: Props = $props()
  let slow = $state(false)
  const shown = $derived(connection === 'updating' && !slow ? 'connected' : connection)
  /** A notice takes the hint area until it clears. */
  const noticed = $derived(status.notice !== '')

  $effect(() => {
    if (connection !== 'updating') return
    const timer = setTimeout(() => (slow = true), updatingDelayMs)
    return () => {
      clearTimeout(timer)
      slow = false
    }
  })
</script>

{#snippet pendingStatus()}
  <span class="mf-status-line__pending" role="status" aria-label="Pending profile changes"
    >{pending}</span
  >
{/snippet}

<div class="mf-status-line">
  <StatusBar>
    {#snippet left()}
      <span class="mf-status-line__hints">
        <span class="mf-status-line__notice" role="status" aria-label="Notice">{status.notice}</span
        >
        {#if compact && !noticed}
          {#if pending}{@render pendingStatus()}{/if}
        {:else if !noticed}
          {#each hints as hint (hint.key)}
            <span class="mf-status-line__hint"><KbdBadge keys={[hint.key]} />{hint.label}</span>
          {/each}
        {/if}
      </span>
    {/snippet}
    {#snippet right()}
      <span class="mf-status-line__state">
        {#if pending && !compact}{@render pendingStatus()}{/if}
        <span class="mf-status-line__connection">
          {#if shown === 'updating'}
            <StatusDot status="working" label="" animated />Updating…
          {:else if shown === 'offline'}
            <StatusDot status="unclean" label="" />Offline
            <Button size="sm" onclick={onretry}>Retry</Button>
          {:else if shown === 'failed'}
            <StatusDot status="unclean" label="" />Not updated
            <Button size="sm" onclick={onretry}>Retry</Button>
          {:else}
            <StatusDot status="idle" label="" />Connected
          {/if}
        </span>
        {#if !compact}<span>profile revision {revision}</span>{/if}
      </span>
    {/snippet}
  </StatusBar>
</div>
