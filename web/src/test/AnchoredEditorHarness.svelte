<script lang="ts">
  import AnchoredEditor from '../components/editing/AnchoredEditor.svelte'
  import type { OverlayState } from '../lib/shell/overlay-state.svelte'

  /** Hosts a stand-in editor body with the title and count the dialog names itself by. */
  interface Props {
    anchor: HTMLElement | undefined
    above?: boolean
    overlays: OverlayState
    oncancel: () => void
    ondismiss: () => void
  }

  let { anchor, above = false, overlays, oncancel, ondismiss }: Props = $props()
</script>

<AnchoredEditor {anchor} {above} field="merchant" id="editor" {overlays} {oncancel} {ondismiss}>
  <h2 id="editor-title">Edit merchant</h2>
  <span id="editor-count">· 1 transaction</span>
  <input aria-label="Merchant" />
  <button>Cancel</button>
</AnchoredEditor>
