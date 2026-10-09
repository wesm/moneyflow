<script module lang="ts">
  export type Overlay =
    | 'search'
    | 'filters'
    | 'help'
    | 'merchant'
    | 'category'
    | 'categories'
    | 'groups'
    | 'review'
    | 'write'
    | 'duplicates'
    | 'delete'
    | 'export'
    | 'information'
</script>

<script lang="ts">
  import FiltersDialog from '../FiltersDialog.svelte'
  import HelpDialog from '../HelpDialog.svelte'
  import SearchOverlay from '../SearchOverlay.svelte'
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
  import type { TransactionInformationResponse, ViewProjection } from '../../lib/api/client'
  import type { ViewController } from '../../lib/controller/view-controller.svelte'

  interface Props {
    controller: ViewController
    projection: ViewProjection
    overlay: Overlay | undefined
    row: { identity: string; kind: 'transaction' | 'aggregate' } | undefined
    deleteCount: number
    deleting: boolean
    information: TransactionInformationResponse | undefined
    onclose: () => void
    onwrite: () => void
    onconfirmdelete: () => void
    onreconnect?: (() => void) | undefined
  }

  let {
    controller,
    projection,
    overlay,
    row,
    deleteCount,
    deleting,
    information,
    onclose,
    onwrite,
    onconfirmdelete,
    onreconnect,
  }: Props = $props()
</script>

{#if overlay === 'search'}
  <SearchOverlay {controller} {onclose} />
{:else if overlay === 'filters'}
  <FiltersDialog {projection} onapply={(action) => controller.apply(action)} {onclose} />
{:else if overlay === 'help'}
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
{:else if overlay === 'delete' && deleting}
  <DeleteConfirmation
    count={deleteCount}
    submitting={controller.editing.state.phase === 'submitting'}
    onconfirm={onconfirmdelete}
    oncancel={onclose}
  />
{:else if overlay === 'export'}
  <ExportDialog controller={controller.export} {onclose} />
{:else if overlay === 'information' && information}
  <TransactionInformationDrawer {information} {onclose} />
{/if}
