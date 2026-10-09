<script lang="ts">
  import type { ViewController } from '../../lib/controller/view-controller.svelte'

  interface Props {
    controller: ViewController
    /** Chart rail loading or failure text; the rail itself can unmount. */
    chartMessage: string
  }

  let { controller, chartMessage }: Props = $props()
</script>

<p class="kit-sr-only" aria-live="polite">{controller.announcement}</p>
<p class="kit-sr-only" aria-live="polite">{controller.editing.state.announcement}</p>
<p class="kit-sr-only" aria-live="polite">{controller.provider.state.announcement}</p>
<p class="kit-sr-only" aria-live="polite">{controller.providerWrite.state.announcement}</p>
<p class="kit-sr-only" aria-live="polite">{controller.duplicates.state.announcement}</p>
<p class="kit-sr-only" aria-live="polite">{controller.export.state.announcement}</p>
<p class="kit-sr-only" aria-live="polite" data-testid="chart-announcement">{chartMessage}</p>
