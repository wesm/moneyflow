<script lang="ts">
  import type { Snippet } from 'svelte'

  interface Props {
    /** Where the view is: the breadcrumb trail and, inside a drill, Break down by. */
    leading: Snippet
    /** View controls such as the period. */
    center?: Snippet | undefined
    /** Display switches at the right edge. */
    trailing?: Snippet | undefined
    /** A full-width row under the bar, such as the search field at phone width. */
    below?: Snippet | undefined
    /**
     * Whether the bar is the page's banner. Where the sidebar shows its top bar, that bar is the
     * banner and this one is the View controls region.
     */
    banner?: boolean
  }

  let { leading, center, trailing, below, banner = true }: Props = $props()
</script>

<div
  class="mf-command-bar"
  role={banner ? 'banner' : 'region'}
  aria-label={banner ? undefined : 'View controls'}
>
  <div class="mf-command-bar__leading">{@render leading()}</div>
  {#if center || trailing}
    <div class="mf-command-bar__controls">
      {#if center}<div class="mf-command-bar__center">{@render center()}</div>{/if}
      {#if trailing}<div class="mf-command-bar__trailing">{@render trailing()}</div>{/if}
    </div>
  {/if}
  {#if below}{@render below()}{/if}
</div>
