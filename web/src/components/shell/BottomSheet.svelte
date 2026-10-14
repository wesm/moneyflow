<script lang="ts">
  import { IconButton } from '@kenn-io/kit-ui'
  import { backdropCloses } from '@kenn-io/kit-ui/utils/overlay'
  import XIcon from '@lucide/svelte/icons/x'
  import type { Snippet } from 'svelte'

  import { trapEveryTab } from '../../lib/shell/focus'
  import type { OverlayState } from '../../lib/shell/overlay-state.svelte'

  interface Props {
    open: boolean
    /** Heading and accessible name of the sheet, in a header with a close button. */
    title?: string | undefined
    /** Without a title, the ID of the heading in the content that names the sheet. */
    labelledby?: string | undefined
    describedby?: string | undefined
    /** `alertdialog` for a confirmation. */
    role?: 'dialog' | 'alertdialog'
    /** Takes Escape on the panel while the sheet is the innermost overlay. */
    overlays: OverlayState
    /** Called on Escape, a press on the scrim, or the close button. */
    onclose: () => void
    children: Snippet
    /** Optional action row pinned below the scrolling body. */
    footer?: Snippet | undefined
  }

  let {
    open,
    title,
    labelledby,
    describedby,
    role = 'dialog',
    overlays,
    onclose,
    children,
    footer,
  }: Props = $props()
  const uid = $props.id()
  const headingId = `mf-sheet-title-${uid}`
</script>

{#if open}
  <!-- kit-ui has no bottom sheet; this composes its focus trap (upstream candidate). -->
  <div class="mf-sheet" role="presentation" {@attach backdropCloses(onclose)}>
    <div
      class="mf-sheet__panel"
      {role}
      aria-modal="true"
      aria-labelledby={labelledby ?? headingId}
      aria-describedby={describedby}
      tabindex="-1"
      {@attach trapEveryTab}
      {@attach overlays.escapeLayer(() => onclose())}
    >
      {#if title !== undefined}
        <div class="mf-sheet__header">
          <h2 class="mf-sheet__title" id={headingId}>{title}</h2>
          <IconButton
            ariaLabel="Close"
            title="Close (Esc)"
            class="mf-touch-target"
            onclick={onclose}
          >
            <XIcon size={16} aria-hidden="true" />
          </IconButton>
        </div>
      {/if}
      <div class="mf-sheet__body">{@render children()}</div>
      {#if footer}<div class="mf-sheet__footer">{@render footer()}</div>{/if}
    </div>
  </div>
{/if}
