<script lang="ts">
  import type { ViewController } from '../../lib/controller/view-controller.svelte'

  interface Props {
    controller: ViewController
  }

  let { controller }: Props = $props()
</script>

<p class="kit-sr-only" aria-live="polite">{controller.announcement}</p>
<p class="kit-sr-only" aria-live="polite">{controller.editing.state.announcement}</p>
<p class="kit-sr-only" aria-live="polite">{controller.provider.state.announcement}</p>
<p class="kit-sr-only" aria-live="polite">{controller.providerWrite.state.announcement}</p>
<p class="kit-sr-only" aria-live="polite">{controller.duplicates.state.announcement}</p>
<p class="kit-sr-only" aria-live="polite">{controller.export.state.announcement}</p>
