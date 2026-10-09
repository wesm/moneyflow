<script lang="ts">
  import type { Snippet } from 'svelte'

  interface Props {
    /** Where the view is: the breadcrumb trail and, inside a drill, Break down by. */
    leading: Snippet
    /** View controls such as the period. */
    center?: Snippet | undefined
    /** Display switches at the right edge. */
    trailing?: Snippet | undefined
  }

  let { leading, center, trailing }: Props = $props()
</script>

<!-- kit CollapsibleSidebar wraps the main area in a <section>, where <header> is no banner. -->
<div class="mf-command-bar" role="banner">
  <div class="mf-command-bar__leading">{@render leading()}</div>
  {#if center || trailing}
    <div class="mf-command-bar__controls">
      {#if center}<div class="mf-command-bar__center">{@render center()}</div>{/if}
      {#if trailing}<div class="mf-command-bar__trailing">{@render trailing()}</div>{/if}
    </div>
  {/if}
</div>
