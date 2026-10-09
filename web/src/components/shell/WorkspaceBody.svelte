<script lang="ts">
  import FinanceGrid from '../table/FinanceGrid.svelte'
  import EmptyScope from './EmptyScope.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { TransitionAction } from '../../lib/controller/view-controller.svelte'
  import {
    moveCursor,
    openEditor,
    openTransactionInformation,
    type LocalActionContext,
  } from '../../lib/shell/local-actions'
  import { emptyScope } from '../../lib/shell/period'

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
</script>

<!-- The workspace's one vertical scroll surface; the table header sticks to its top. -->
<div
  class="mf-workspace"
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
      {viewport}
      compact={false}
      onmove={(delta) => moveCursor(() => controller.moveCursor(delta))}
      onhome={() => moveCursor(() => controller.moveHome())}
      oncursor={(index) => moveCursor(() => controller.moveCursorTo(index))}
      ontoggle={(identity, kind) =>
        onapply({ action: 'selection.toggle', target: { identity, kind } })}
      ontoggleall={() => onapply({ action: 'selection.toggle-all' })}
      onactivate={(identity, kind) => onapply({ action: 'view.drill', target: { identity, kind } })}
      oninformation={(identity) => void openTransactionInformation(actions, identity)}
      onedit={(identity, field) => openEditor(actions, field, { identity, kind: 'transaction' })}
      onsort={(field) => onapply({ action: 'sort.set', sort_field: field })}
      onshow={(index) => void controller.showIndex(index)}
    />
  {/if}
</div>
