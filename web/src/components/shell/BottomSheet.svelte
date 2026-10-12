<script lang="ts">
  import { IconButton } from '@kenn-io/kit-ui'
  import { trapFocus } from '@kenn-io/kit-ui/utils/focus-trap'
  import { backdropCloses, escapeCloses } from '@kenn-io/kit-ui/utils/overlay'
  import XIcon from '@lucide/svelte/icons/x'
  import type { Snippet } from 'svelte'

  interface Props {
    open: boolean
    /** Heading and accessible name of the sheet. */
    title: string
    /** Called on Escape, a press on the scrim, or the close button. */
    onclose: () => void
    children: Snippet
    /** Optional action row pinned below the scrolling body. */
    footer?: Snippet | undefined
  }

  let { open, title, onclose, children, footer }: Props = $props()
  const uid = $props.id()
  const headingId = `mf-sheet-title-${uid}`
  const escape = escapeCloses(() => onclose())
</script>

<svelte:window onkeydown={open ? escape : undefined} />

{#if open}
  <!-- kit-ui has no bottom sheet; this composes its focus trap (upstream candidate). -->
  <div class="mf-sheet" role="presentation" onpointerdown={backdropCloses(onclose)}>
    <div
      class="mf-sheet__panel"
      role="dialog"
      aria-modal="true"
      aria-labelledby={headingId}
      tabindex="-1"
      {@attach trapFocus}
    >
      <div class="mf-sheet__header">
        <h2 class="mf-sheet__title" id={headingId}>{title}</h2>
        <IconButton ariaLabel="Close" title="Close (Esc)" class="mf-touch-target" onclick={onclose}>
          <XIcon size={16} aria-hidden="true" />
        </IconButton>
      </div>
      <div class="mf-sheet__body">{@render children()}</div>
      {#if footer}<div class="mf-sheet__footer">{@render footer()}</div>{/if}
    </div>
  </div>
{/if}
