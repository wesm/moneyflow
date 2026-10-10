<script lang="ts">
  import HelpDialog from '../HelpDialog.svelte'
  import TransactionInformationDrawer from '../TransactionInformationDrawer.svelte'
  import AnchoredEditor from '../editing/AnchoredEditor.svelte'
  import CategoryManager from '../editing/CategoryManager.svelte'
  import DeleteConfirmation from '../editing/DeleteConfirmation.svelte'
  import DuplicateReview from '../editing/DuplicateReview.svelte'
  import ExportDialog from '../editing/ExportDialog.svelte'
  import GroupManager from '../editing/GroupManager.svelte'
  import EditorBody from '../editing/EditorBody.svelte'
  import ReviewDrawer from '../editing/ReviewDrawer.svelte'
  import WriteStatusDrawer from '../editing/WriteStatusDrawer.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import { providerName } from '../../lib/provider-name'
  import type { ViewController } from '../../lib/controller/view-controller.svelte'
  import type { OverlayState } from '../../lib/shell/overlay-state.svelte'

  interface Props {
    controller: ViewController
    projection: ViewProjection
    overlays: OverlayState
    onconfirmdelete: () => void
    onreconnect?: (() => void) | undefined
  }

  let { controller, projection, overlays, onconfirmdelete, onreconnect }: Props = $props()
  const overlay = $derived(overlays.current)
  const information = $derived(overlays.information)
  const target = $derived(overlays.editTarget)
  const onclose = () => overlays.close()
  const onwrite = () => overlays.open('write')
</script>

{#if overlay === 'help'}
  <HelpDialog capabilities={projection.capabilities ?? []} {onclose} />
{:else if (overlay === 'merchant' || overlay === 'category') && target}
  <AnchoredEditor
    anchor={overlays.anchor}
    above={projection.selection_count > 0}
    field={overlay}
    id="mf-editor"
    {overlays}
    oncancel={onclose}
    ondismiss={() => overlays.close('keep')}
  >
    <EditorBody
      kind={overlay}
      {target}
      controller={controller.editing}
      {projection}
      cursorIndex={controller.cursorIndex}
      id="mf-editor"
      onstage={onclose}
      oncancel={onclose}
    />
  </AnchoredEditor>
{:else if overlay === 'categories'}
  <CategoryManager controller={controller.editing} {onclose} />
{:else if overlay === 'groups'}
  <GroupManager controller={controller.editing} {onclose} />
{:else if overlay === 'review'}
  <ReviewDrawer editing={controller.editing} review={controller.review} {onclose} {onwrite} />
{:else if overlay === 'write'}
  <WriteStatusDrawer
    controller={controller.providerWrite}
    providerName={providerName(projection.profile_kind)}
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
