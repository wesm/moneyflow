<script lang="ts">
  import { Button, KbdBadge, Tooltip } from '@kenn-io/kit-ui'
  import EyeOffIcon from '@lucide/svelte/icons/eye-off'
  import StoreIcon from '@lucide/svelte/icons/store'
  import TagIcon from '@lucide/svelte/icons/tag'
  import Trash2Icon from '@lucide/svelte/icons/trash-2'
  import XIcon from '@lucide/svelte/icons/x'

  import type { ViewProjection } from '../../lib/api/client'
  import { selectionButtons, selectionText, type SelectionButton } from '../../lib/table/selection'

  interface Props {
    projection: ViewProjection
    /** Runs a button's action: the same local action as its key, or Clear. */
    onaction: (action: SelectionButton['action']) => void
    /** The bar's height in pixels, so the table keeps its rows above it. */
    height?: number
  }

  let { projection, onaction, height = $bindable(0) }: Props = $props()
  const text = $derived(selectionText(projection))
  const buttons = $derived(selectionButtons(projection))
  const icons: Record<SelectionButton['action'], typeof StoreIcon> = {
    'edit.merchant': StoreIcon,
    'edit.category': TagIcon,
    'edit.hide': EyeOffIcon,
    'edit.delete': Trash2Icon,
    clear: XIcon,
  }
</script>

{#snippet action(button: SelectionButton)}
  {@const Icon = icons[button.action]}
  <Button
    size="sm"
    class="mf-selection-bar__button"
    disabled={button.reason !== undefined}
    ariaLabel={button.label}
    label={button.label}
    onclick={() => onaction(button.action)}
  >
    <Icon size={14} aria-hidden="true" />
    {#snippet trailing()}<KbdBadge keys={[button.key]} />{/snippet}
  </Button>
{/snippet}

<section class="mf-selection-bar" aria-label="Selection" bind:offsetHeight={height}>
  <span class="mf-selection-bar__summary">{text}</span>
  <div class="mf-selection-bar__actions">
    {#each buttons as button (button.action)}
      {#if button.reason}
        <Tooltip text={button.reason} align="end">{@render action(button)}</Tooltip>
      {:else}
        {@render action(button)}
      {/if}
    {/each}
  </div>
</section>
