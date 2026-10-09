<script lang="ts">
  import type { ViewProjection } from '../../lib/api/client'
  import { breadcrumbTrail } from '../../lib/shell/breadcrumbs'

  interface Props {
    projection: ViewProjection
    /** Returns to the level that keeps `depth` drill frames. */
    onback: (depth: number) => void
  }

  let { projection, onback }: Props = $props()
  const trail = $derived(breadcrumbTrail(projection))
</script>

<!-- kit-ui has no breadcrumb component; the trail composes plain buttons (upstream candidate). -->
<nav class="mf-breadcrumbs" aria-label="Breadcrumb">
  <ol>
    {#each trail as crumb, index (index)}
      <li>
        {#if index > 0}<span class="mf-breadcrumbs__separator" aria-hidden="true">›</span>{/if}
        {#if crumb.depth === undefined}
          <span class="mf-breadcrumbs__current" aria-current="page">{crumb.label}</span>
        {:else}
          {@const depth = crumb.depth}
          <button class="mf-breadcrumbs__link" type="button" onclick={() => onback(depth)}>
            {crumb.label}
          </button>
        {/if}
      </li>
    {/each}
  </ol>
</nav>
