<script lang="ts">
  import HelpDialog from '../HelpDialog.svelte'
  import TransactionInformationDrawer from '../TransactionInformationDrawer.svelte'
  import CategoryDialog from '../editing/CategoryDialog.svelte'
  import CategoryManager from '../editing/CategoryManager.svelte'
  import DeleteConfirmation from '../editing/DeleteConfirmation.svelte'
  import DuplicateReview from '../editing/DuplicateReview.svelte'
  import ExportDialog from '../editing/ExportDialog.svelte'
  import GroupManager from '../editing/GroupManager.svelte'
  import MerchantDialog from '../editing/MerchantDialog.svelte'
  import ReviewDrawer from '../editing/ReviewDrawer.svelte'
  import WriteStatusDrawer from '../editing/WriteStatusDrawer.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { ViewController } from '../../lib/controller/view-controller.svelte'
  import type { OverlayState } from '../../lib/shell/overlay-state.svelte'

  interface Props {
    controller: ViewController
    projection: ViewProjection
    overlays: OverlayState
    row: { identity: string; kind: 'transaction' | 'aggregate' } | undefined
    onconfirmdelete: () => void
    onreconnect?: (() => void) | undefined
  }

  let { controller, projection, overlays, row, onconfirmdelete, onreconnect }: Props = $props()
  const overlay = $derived(overlays.current)
  const information = $derived(overlays.information)
  const onclose = () => overlays.close()
  const onwrite = () => overlays.open('write')
</script>

{#if overlay === 'help'}
  <HelpDialog capabilities={projection.capabilities ?? []} {onclose} />
{:else if overlay === 'merchant' && row}
  <MerchantDialog
    controller={controller.editing}
    target={row}
    hasSelection={projection.selection_count > 0}
    {onclose}
  />
{:else if overlay === 'category' && row}
  <CategoryDialog controller={controller.editing} target={row} {onclose} />
{:else if overlay === 'categories'}
  <CategoryManager controller={controller.editing} {onclose} />
{:else if overlay === 'groups'}
  <GroupManager controller={controller.editing} {onclose} />
{:else if overlay === 'review'}
  <ReviewDrawer editing={controller.editing} review={controller.review} {onclose} {onwrite} />
{:else if overlay === 'write'}
  <WriteStatusDrawer
    controller={controller.providerWrite}
    providerName={projection.profile_kind === 'ynab' ? 'YNAB' : 'Monarch'}
    {onclose}
    {onreconnect}
  />
{:else if overlay === 'duplicates'}
  <DuplicateReview controller={controller.duplicates} {onclose} />
{:else if overlay === 'delete' && overlays.deleteTarget}
  <DeleteConfirmation
    count={overlays.deleteCount}
    submitting={controller.editing.state.phase === 'submitting'}
    onconfirm={onconfirmdelete}
    oncancel={onclose}
  />
{:else if overlay === 'export'}
  <ExportDialog controller={controller.export} {onclose} />
{:else if overlay === 'information' && information}
  <TransactionInformationDrawer {information} {onclose} />
{/if}
