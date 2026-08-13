<script lang="ts">
  import { Button, KbdBadge } from '@kenn-io/kit-ui'
  import type { ViewProjection } from '../lib/api/client'
  interface Props {
    projection: ViewProjection
    onaction: (action: string) => void
  }
  let { projection, onaction }: Props = $props()
</script>

<nav class="refinement-bar" aria-label="Active refinements">
  <div class="refinement-bar__trail">
    {#each projection.breadcrumbs as breadcrumb, index (`${breadcrumb.dimension}:${breadcrumb.label}`)}
      {#if index > 0}<span aria-hidden="true">/</span>{/if}<span>{breadcrumb.label}</span>
    {/each}
  </div>
  <span>{projection.total_rows} results</span>
  <span>Group: {projection.view.grouping}</span>
  {#if projection.view.search}<span>Search: {projection.view.search}</span>{/if}
  <span>Sort: {projection.view.sort_field} {projection.view.sort_direction}</span>
  <Button size="sm" onclick={() => onaction('view.back')}>Clear <KbdBadge keys={['Esc']} /></Button>
</nav>
