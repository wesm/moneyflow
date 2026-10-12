<script lang="ts">
  import type { ViewProjection } from '../../lib/api/client'
  import type { RailLoader } from '../../lib/shell/rail-loader.svelte'

  interface Props {
    loader: RailLoader
    projection: ViewProjection
    cursorIndex: number
    oncursor: (index: number) => void
    ondrill: (identity: string) => void
  }

  let { loader, projection, cursorIndex, oncursor, ondrill }: Props = $props()
</script>

{#if loader.component}
  <loader.component {projection} {cursorIndex} {oncursor} {ondrill} />
{:else}
  <!-- AppShell announces this text through a live region that stays mounted. -->
  <p class="overlay-status" class:overlay-status--error={loader.status === 'failed'}>
    {loader.message}
  </p>
{/if}
