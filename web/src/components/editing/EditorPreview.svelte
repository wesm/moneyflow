<script lang="ts">
  import type { MerchantPreview } from '../../lib/api/client'

  interface Props {
    /** The impact notice, one sentence per line. */
    impact: string[]
    /** The affected transactions, once loaded. */
    preview: MerchantPreview | undefined
    /** Why the preview could not load. */
    error: string
  }

  let { impact, preview, error }: Props = $props()
</script>

{#each impact as line (line)}<p class="mf-editor-preview__line">{line}</p>{/each}
{#if preview}
  <ul class="mf-editor-preview" aria-label="Affected transactions">
    {#each preview.transactions ?? [] as row (row.identity)}
      <li><span>{row.date}</span><span>{row.amount.display} {row.amount.currency}</span></li>
    {/each}
  </ul>
{:else if error}
  <p class="mf-editor-preview__line editing-error" role="alert">{error}</p>
{:else}
  <p class="mf-editor-preview__line mf-editor-preview__quiet">Loading affected transactions…</p>
{/if}

<style>
  .mf-editor-preview__line {
    margin: 0;
    font-size: var(--font-size-sm);
  }
  .mf-editor-preview__quiet {
    color: var(--text-secondary);
    font-size: var(--font-size-xs);
  }
  .mf-editor-preview {
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: var(--font-size-sm);
    font-variant-numeric: tabular-nums;
  }
  .mf-editor-preview li {
    display: flex;
    justify-content: space-between;
    gap: var(--space-2);
  }
</style>
