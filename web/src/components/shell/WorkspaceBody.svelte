<script lang="ts">
  import FinanceGrid from '../table/FinanceGrid.svelte'
  import EmptyScope from './EmptyScope.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { TransitionAction } from '../../lib/controller/view-controller.svelte'
  import {
    openEditor,
    openTransactionInformation,
    type LocalActionContext,
  } from '../../lib/shell/local-actions'
  import { emptyScope } from '../../lib/shell/period'
  import { status } from '../../lib/shell/status.svelte'

  interface Props {
    projection: ViewProjection
    actions: LocalActionContext
    /** Applies a view transition from the table, then returns focus to it. */
    onapply: (request: TransitionAction) => void
  }

  let { projection, actions, onapply }: Props = $props()
  const controller = $derived(actions.controller)
  const empty = $derived(emptyScope(projection))

  /** Moves the cursor. The grid handles j, k, and Home itself, so it clears the notice here. */
  function moveCursor(move: () => Promise<void>): void {
    status.clear()
    void move()
  }
</script>

{#if empty}
  <EmptyScope
    scope={empty}
    onjump={() => onapply({ action: 'time.set-period', period: empty.jump })}
  />
{:else}
  <FinanceGrid
    {projection}
    cursorIndex={controller.cursorIndex}
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
  />
{/if}
