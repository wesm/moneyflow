<script lang="ts">
  import { Button } from '@kenn-io/kit-ui'
  import EyeOffIcon from '@lucide/svelte/icons/eye-off'
  import InfoIcon from '@lucide/svelte/icons/info'
  import StoreIcon from '@lucide/svelte/icons/store'
  import TagIcon from '@lucide/svelte/icons/tag'
  import Trash2Icon from '@lucide/svelte/icons/trash-2'

  import BottomSheet from '../shell/BottomSheet.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { components } from '../../lib/api/schema'
  import { rowActionButtons, type RowActionButton } from '../../lib/table/actions'

  interface Props {
    open: boolean
    /** The transaction row whose ⋯ button opened the sheet. */
    row: components['schemas']['DetailRow']
    projection: ViewProjection
    /** Runs an item on the row, as its key runs on the cursor row. */
    onaction: (action: RowActionButton['action']) => void
    onclose: () => void
  }

  let { open, row, projection, onaction, onclose }: Props = $props()
  const uid = $props.id()
  const buttons = $derived(rowActionButtons(projection))
  const icons: Record<RowActionButton['action'], typeof StoreIcon> = {
    'edit.merchant': StoreIcon,
    'edit.category': TagIcon,
    'edit.hide': EyeOffIcon,
    'edit.delete': Trash2Icon,
    'transaction.info': InfoIcon,
  }
</script>

<BottomSheet {open} title={row.merchant} {onclose}>
  <p class="mf-row-actions__summary">{row.date} · {row.amount.display}</p>
  <ul class="mf-row-actions">
    {#each buttons as button (button.action)}
      {@const Icon = icons[button.action]}
      {@const reasonID = `mf-row-actions-${uid}-${button.key}`}
      <li>
        <Button
          class="mf-row-actions__button"
          disabled={button.reason !== undefined}
          ariaDescribedby={button.reason ? reasonID : undefined}
          label={button.label}
          onclick={() => onaction(button.action)}
        >
          <Icon size={16} aria-hidden="true" />
        </Button>
        {#if button.reason}<p class="mf-row-actions__reason" id={reasonID}>{button.reason}</p>{/if}
      </li>
    {/each}
  </ul>
</BottomSheet>
