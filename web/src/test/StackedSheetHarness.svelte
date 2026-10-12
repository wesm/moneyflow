<script lang="ts">
  import { Modal } from '@kenn-io/kit-ui'

  import BottomSheet from '../components/shell/BottomSheet.svelte'
  import type { OverlayState } from '../lib/shell/overlay-state.svelte'

  /** An open sheet with a kit `Modal` stacked above it, as a confirmation would be. */
  interface Props {
    overlays: OverlayState
    onclosesheet: () => void
    onclosemodal: () => void
  }

  let { overlays, onclosesheet, onclosemodal }: Props = $props()
</script>

<BottomSheet open title="View" {overlays} onclose={onclosesheet}>
  <button type="button">First</button>
</BottomSheet>
<Modal title="Confirm" onclose={onclosemodal}>
  <button type="button">Confirm</button>
</Modal>
