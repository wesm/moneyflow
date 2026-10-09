<script lang="ts">
  import { Button, KbdBadge, StatusBar, StatusDot } from '@kenn-io/kit-ui'

  import type { KeyHint } from '../../lib/shell/key-hints'

  export type Connection = 'connected' | 'updating' | 'offline'

  interface Props {
    hints: KeyHint[]
    connection: Connection
    revision: string
    /** Pending text, given when the sidebar card is not showing it. */
    pending?: string | undefined
    /** Phone width: the pending text takes the hint area, which has no keyboard to hint at. */
    compact?: boolean
    onretry: () => void
  }

  let { hints, connection, revision, pending, compact = false, onretry }: Props = $props()
</script>

{#snippet pendingStatus()}
  <span class="mf-status-line__pending" role="status" aria-label="Pending profile changes"
    >{pending}</span
  >
{/snippet}

<!-- Inside kit CollapsibleSidebar's <section>, the StatusBar <footer> is no contentinfo. -->
<div class="mf-status-line" role="contentinfo">
  <StatusBar>
    {#snippet left()}
      <span class="mf-status-line__hints">
        {#if compact}
          {#if pending}{@render pendingStatus()}{/if}
        {:else}
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
          {#if connection === 'updating'}
            <StatusDot status="working" label="" />Updating…
          {:else if connection === 'offline'}
            <StatusDot status="unclean" label="" />Offline
            <Button size="sm" onclick={onretry}>Retry</Button>
          {:else}
            <StatusDot status="idle" label="" />Connected
          {/if}
        </span>
        <span>profile revision {revision}</span>
      </span>
    {/snippet}
  </StatusBar>
</div>
