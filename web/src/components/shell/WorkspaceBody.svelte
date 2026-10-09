<script lang="ts">
  import FinanceGrid from '../table/FinanceGrid.svelte'
  import SelectionBar from '../table/SelectionBar.svelte'
  import EmptyScope from './EmptyScope.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { TransitionAction } from '../../lib/controller/view-controller.svelte'
  import {
    localActions,
    moveCursor,
    openEditor,
    openTransactionInformation,
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
  /** A Clear is queued; another click must not also go back a level. */
  let clearing = false

  /** Runs a selection bar button as its key does, then leaves focus on the table. */
  function select(action: SelectionButton['action']): void {
    if (action === 'clear') {
      if (clearing) return
      clearing = true
      onapply({ action: 'view.back' })
      const settle = () => (clearing = false)
      return void actions.afterTransitions().then(settle, settle)
    }
    scroller?.querySelector<HTMLElement>('[role="grid"]')?.focus({ preventScroll: true })
    status.clear()
    local[action]()
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
    {#if selecting}
      <SelectionBar {projection} onaction={select} bind:height={barHeight} />
    {/if}
  {/if}
</div>
