<script lang="ts">
  import AnchoredEditor from '../components/editing/AnchoredEditor.svelte'
  import BottomSheet from '../components/shell/BottomSheet.svelte'
  import type { OverlayState } from '../lib/shell/overlay-state.svelte'

  /**
   * Hosts a stand-in editor body with the title and count the dialog names itself by. Its field
   * takes focus when it mounts, as the editor's does. With `compact`, a bottom sheet hosts it, as
   * at phone width.
   */
  interface Props {
    anchor: HTMLElement | undefined
    above?: boolean
    compact?: boolean
    overlays: OverlayState
    oncancel: () => void
    ondismiss: () => void
  }

  let { anchor, above = false, compact = false, overlays, oncancel, ondismiss }: Props = $props()
</script>

{#snippet body()}
  <h2 id="editor-title">Edit merchant</h2>
  <span id="editor-count">· 1 transaction</span>
  <input aria-label="Merchant" {@attach (input) => input.focus()} />
  <button>Cancel</button>
{/snippet}

{#if compact}
  <BottomSheet
    open
    labelledby="editor-title"
    describedby="editor-count"
    {overlays}
    onclose={oncancel}
  >
    {@render body()}
  </BottomSheet>
{:else}
  <AnchoredEditor {anchor} {above} field="merchant" id="editor" {overlays} {oncancel} {ondismiss}>
    {@render body()}
  </AnchoredEditor>
{/if}
