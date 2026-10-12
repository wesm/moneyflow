<script lang="ts">
  import { Button } from '@kenn-io/kit-ui'
  import type { Snippet } from 'svelte'

  interface Props {
    /** An insights request failed; the line offers Retry and never blocks the table. */
    failed: boolean
    /** The content belongs to an earlier view or revision: dimmed and out of reach. */
    stale: boolean
    onretry: () => void
    /** What to show, if anything, besides the failure line. */
    content?: Snippet | undefined
  }

  let { failed, stale, onretry, content }: Props = $props()
</script>

<!-- Rendered, not announced: a failure needs no interruption while the table keeps working. -->
{#if failed}
  <p class="mf-chart-failure">
    Couldn't load charts ·
    <Button size="sm" onclick={onretry}>Retry</Button>
  </p>
{/if}
{#if content}
  <div class={['mf-chart-states', stale && 'mf-chart-states--stale']} inert={stale}>
    {@render content()}
  </div>
{/if}
