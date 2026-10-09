<script lang="ts">
  import { IconButton } from '@kenn-io/kit-ui'
  import { trapFocus } from '@kenn-io/kit-ui/utils/focus-trap'
  import { backdropCloses, escapeCloses } from '@kenn-io/kit-ui/utils/overlay'
  import XIcon from '@lucide/svelte/icons/x'
  import type { Snippet } from 'svelte'

  interface Props {
    open: boolean
    /** Called on Escape, a press on the scrim, or the close button. */
    onclose: () => void
    /** The sidebar. */
    children: Snippet
  }

  let { open, onclose, children }: Props = $props()
  const escape = escapeCloses(() => onclose())
</script>

<svelte:window onkeydown={open ? escape : undefined} />

{#if open}
  <!-- kit-ui-check-ignore: kit-ui has no left navigation drawer; this composes its focus trap. -->
  <div class="mf-nav-drawer" role="presentation" onpointerdown={backdropCloses(onclose)}>
    <div
      class="mf-nav-drawer__panel"
      role="dialog"
      aria-modal="true"
      aria-label="Navigation"
      tabindex="-1"
      {@attach trapFocus}
    >
      {@render children()}
      <IconButton
        ariaLabel="Close navigation"
        title="Close (Esc)"
        class="mf-nav-drawer__close mf-touch-target"
        onclick={onclose}
      >
        <XIcon size={16} aria-hidden="true" />
      </IconButton>
    </div>
  </div>
{/if}
