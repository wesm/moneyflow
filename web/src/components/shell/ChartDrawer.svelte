<script lang="ts">
  import { DetailDrawer } from '@kenn-io/kit-ui'
  import type { Snippet } from 'svelte'

  interface Props {
    open: boolean
    onclose: () => void
    children: Snippet
  }

  let { open, onclose, children }: Props = $props()
</script>

{#if open}
  <DetailDrawer
    title="Visualizations"
    ariaLabel="Moneyflow visualizations"
    width="min(560px, calc(100vw - 1px))"
    {onclose}
  >
    <div class="mf-chart-dialog">
      {@render children()}
    </div>
  </DetailDrawer>
{/if}
