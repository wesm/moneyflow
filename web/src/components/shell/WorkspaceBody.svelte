<script lang="ts">
  import { MEDIA } from '@kenn-io/kit-ui'
  import { untrack } from 'svelte'
  import { MediaQuery } from 'svelte/reactivity'

  import FinanceGrid from '../table/FinanceGrid.svelte'
  import RowActionsSheet from '../table/RowActionsSheet.svelte'
  import SelectionBar from '../table/SelectionBar.svelte'
  import EmptyScope from './EmptyScope.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { components } from '../../lib/api/schema'
  import type { TransitionAction } from '../../lib/controller/view-controller.svelte'
  import {
    localActions,
    moveCursor,
    openEditorWhenSettled,
    openTransactionInformation,
    runRowAction,
    type LocalActionContext,
  } from '../../lib/shell/local-actions'
  import { emptyScope } from '../../lib/shell/period'
  import { status } from '../../lib/shell/status.svelte'
  import type { SelectionButton } from '../../lib/table/selection'

  interface Props {
    projection: ViewProjection
    actions: LocalActionContext
    /** Applies a view transition from the table, then returns focus to it. */
    onapply: (request: TransitionAction) => void
  }

  let { projection, actions, onapply }: Props = $props()
  const controller = $derived(actions.controller)
  const empty = $derived(emptyScope(projection))
  let scroller = $state<HTMLDivElement>()
  let scrollTop = $state(0)
  let viewport = $state(520)
  let barHeight = $state(0)
  const selecting = $derived(!empty && projection.selection_count > 0)
  /** The bar covers the scroller's bottom, so the table counts only the rows above it. */
  const covered = $derived(selecting ? barHeight : 0)
  const local = $derived(localActions(actions))
  /** A Clear is queued; another click must not queue a second one. */
  let clearing = false
  const compact = new MediaQuery(MEDIA.compact)
  /** The row whose ⋯ button opened the actions sheet. */
  let sheetRow = $state<components['schemas']['DetailRow']>()

  // The ⋯ buttons exist only on phone rows, so growing past 640 px closes their sheet.
  $effect(() => {
    if (!compact.current && untrack(() => actions.overlays.current) === 'actions') {
      actions.overlays.close()
    }
  })

  /** Runs a selection bar button as its key does, then leaves focus on the table. */
  function select(action: SelectionButton['action']): void {
    if (action === 'clear') {
      if (clearing) return
      clearing = true
      const settle = () => (clearing = false)
      return void actions.queue(clearSelection).then(settle, settle)
    }
    actions.focusGrid()
    status.clear()
    local[action]()
  }

  // Back clears a selection. A toggle queued before the Clear can leave none, and Back would
  // then leave the search, sub-grouping, or drill level instead.
  async function clearSelection(): Promise<void> {
    if (controller.projection?.selection_count) await controller.apply({ action: 'view.back' })
    actions.focusGrid()
  }
</script>

<!-- The workspace's one vertical scroll surface; the table header sticks to its top. -->
<div
  class={['mf-workspace', selecting && 'mf-workspace--selecting']}
  style:--mf-selection-height={`${covered}px`}
  bind:this={scroller}
  bind:clientHeight={viewport}
  onscroll={(event) => (scrollTop = event.currentTarget.scrollTop)}
>
  {#if empty}
    <EmptyScope
      scope={empty}
      onjump={() => onapply({ action: 'time.set-period', period: empty.jump })}
    />
  {:else}
    <FinanceGrid
      {projection}
      neighbors={controller.neighbors}
      cursorIndex={controller.cursorIndex}
      {scroller}
      bind:scrollTop
      viewport={viewport - covered}
      compact={compact.current}
      onmove={(delta) => moveCursor(() => controller.moveCursor(delta))}
      onhome={() => moveCursor(() => controller.moveHome())}
      oncursor={(index) => moveCursor(() => controller.moveCursorTo(index))}
      ontoggle={(identity, kind) =>
        onapply({ action: 'selection.toggle', target: { identity, kind } })}
      ontoggleall={() => onapply({ action: 'selection.toggle-all' })}
      onactivate={(identity, kind) => onapply({ action: 'view.drill', target: { identity, kind } })}
      oninformation={(identity) =>
        actions.queue(() => openTransactionInformation(actions, identity))}
      onedit={(identity, field) =>
        openEditorWhenSettled(actions, field, { identity, kind: 'transaction' })}
      onsort={(field) => onapply({ action: 'sort.set', sort_field: field })}
      onactions={(row, opener) => {
        sheetRow = row
        actions.overlays.open('actions', opener)
      }}
      onshow={(index) => void controller.showIndex(index)}
    />
    {#if selecting}
      <SelectionBar {projection} onaction={select} bind:height={barHeight} />
    {/if}
  {/if}
</div>
{#if sheetRow}
  {@const target = { identity: sheetRow.identity, kind: 'transaction' } as const}
  <RowActionsSheet
    open={actions.overlays.current === 'actions'}
    row={sheetRow}
    {projection}
    onaction={(action) => {
      status.clear()
      runRowAction(actions, action, target)
    }}
    onclose={() => actions.overlays.close()}
  />
{/if}
