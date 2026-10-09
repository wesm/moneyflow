<script lang="ts">
  import { Chip, IconButton, StatusDot } from '@kenn-io/kit-ui'

  import type { components } from '../../lib/api/schema'
  import { outOfTabOrder } from '../../lib/table/pointer'

  interface Props {
    row: components['schemas']['DetailRow']
    oninformation?: ((identity: string) => void) | undefined
  }

  let { row, oninformation }: Props = $props()
  const stagedHidden = $derived(row.staged?.hidden)
  const match = $derived(row.amazon_match)
  const strong = $derived(match?.confidence === 'high' || match?.confidence === 'medium')
</script>

<span class="mf-flags">
  {#if row.flags.pending}
    <StatusDot status="stale" label="Pending edits" />
  {/if}
  {#if row.staged?.deleted}
    <Chip size="xs" tone="warning" uppercase={false}>Delete</Chip>
  {:else if stagedHidden === true}
    <Chip size="xs" tone="warning" uppercase={false}>Hidden</Chip>
  {:else if stagedHidden === false}
    <Chip size="xs" tone="warning" uppercase={false}>Visible</Chip>
  {:else if row.flags.hidden}
    <Chip size="xs" tone="muted" uppercase={false}>Hidden</Chip>
  {/if}
  {#if match}
    <span class="mf-flags__match" {@attach outOfTabOrder}>
      <IconButton
        size="sm"
        ariaLabel={`Amazon match: ${match.first_product}, ${match.confidence}`}
        onclick={(event) => {
          event.stopPropagation()
          oninformation?.(row.identity)
        }}>{strong ? '✓' : '~'}</IconButton
      >
    </span>
  {/if}
</span>
