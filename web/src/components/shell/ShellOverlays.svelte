<script lang="ts">
  import { MEDIA } from '@kenn-io/kit-ui'
  import { MediaQuery } from 'svelte/reactivity'

  import BottomSheet from './BottomSheet.svelte'
  import HelpDialog from '../HelpDialog.svelte'
  import TransactionInformationDrawer from '../TransactionInformationDrawer.svelte'
  import AnchoredEditor from '../editing/AnchoredEditor.svelte'
  import CategoryManager from '../editing/CategoryManager.svelte'
  import DeleteConfirm from '../editing/DeleteConfirm.svelte'
  import DuplicateReview from '../editing/DuplicateReview.svelte'
  import ExportDialog from '../editing/ExportDialog.svelte'
  import GroupManager from '../editing/GroupManager.svelte'
  import EditorBody from '../editing/EditorBody.svelte'
  import ReviewDrawer from '../editing/ReviewDrawer.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import { providerName } from '../../lib/provider-name'
  import type { ViewController } from '../../lib/controller/view-controller.svelte'
  import type { OverlayState } from '../../lib/shell/overlay-state.svelte'

  interface Props {
    controller: ViewController
    projection: ViewProjection
    overlays: OverlayState
    /** Stages the confirmed delete. Resolves whether the server accepted it. */
    onconfirmdelete: () => Promise<boolean>
    onreconnect?: (() => void) | undefined
  }

  let { controller, projection, overlays, onconfirmdelete, onreconnect }: Props = $props()
  const overlay = $derived(overlays.current)
  const information = $derived(overlays.information)
  const target = $derived(overlays.editTarget)
  const onclose = () => overlays.close()
  const onwrite = () => overlays.open('write')
  // On phones the editor and the delete confirmation are bottom sheets. Staging an edit returns
  // focus to the edited row, not the ⋯.
  const compact = new MediaQuery(MEDIA.compact)
</script>

{#snippet editor(kind: 'merchant' | 'category', onstage: () => void)}
  {#if target}
    <EditorBody
      {kind}
      {target}
      controller={controller.editing}
      {projection}
      cursorIndex={controller.cursorIndex}
      id="mf-editor"
      {onstage}
      oncancel={onclose}
    />
  {/if}
{/snippet}

{#if overlay === 'help'}
  <HelpDialog capabilities={projection.capabilities ?? []} {onclose} />
{:else if (overlay === 'merchant' || overlay === 'category') && target && compact.current}
  <BottomSheet open labelledby="mf-editor-title" describedby="mf-editor-count" {overlays} {onclose}>
    {@render editor(overlay, () => overlays.close('grid'))}
  </BottomSheet>
{:else if (overlay === 'merchant' || overlay === 'category') && target}
  <AnchoredEditor
    anchor={overlays.anchor?.element}
    above={overlays.anchor?.above ?? false}
    field={overlay}
    id="mf-editor"
    {overlays}
    oncancel={onclose}
    ondismiss={() => overlays.close('keep')}
  >
    {@render editor(overlay, onclose)}
  </AnchoredEditor>
{:else if overlay === 'categories'}
  <CategoryManager controller={controller.editing} {onclose} />
{:else if overlay === 'groups'}
  <GroupManager controller={controller.editing} {onclose} />
{:else if overlay === 'review' || overlay === 'write'}
  <ReviewDrawer
    mode={overlay}
    editing={controller.editing}
    review={controller.review}
    providerWrite={controller.providerWrite}
    providerName={providerName(projection.profile_kind)}
    {onclose}
    {onwrite}
    {onreconnect}
  />
{:else if overlay === 'duplicates'}
  <DuplicateReview controller={controller.duplicates} {onclose} />
{:else if overlay === 'delete' && overlays.deleteTarget}
  <DeleteConfirm
    count={overlays.deleteCount}
    compact={compact.current}
    anchor={overlays.anchor}
    {overlays}
    onconfirm={onconfirmdelete}
    oncancel={onclose}
    ondismiss={() => overlays.close('keep')}
  />
{:else if overlay === 'export'}
  <ExportDialog controller={controller.export} {onclose} />
{:else if overlay === 'information' && information}
  <TransactionInformationDrawer {information} {onclose} />
{/if}
