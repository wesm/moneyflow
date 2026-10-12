<script lang="ts">
  import FinanceTable from '../FinanceTable.svelte'
  import EmptyScope from './EmptyScope.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { ViewController } from '../../lib/controller/view-controller.svelte'
  import type { FocusedRow } from '../../lib/shell/local-actions'
  import { emptyScope, type Period } from '../../lib/shell/period'
  import type { TransitionQueue } from '../../lib/shell/transition-queue'

  interface Props {
    controller: ViewController
    projection: ViewProjection
    /** Orders cursor moves after pending view changes, such as an Esc return. */
    queue: TransitionQueue
    /** Sends a row action such as a drill or a selection toggle. */
    ontarget: (action: string, identity: string, kind: FocusedRow['kind']) => void
    oninformation: (identity: string) => void
    onperiod: (period: Period) => void
  }

  let { controller, projection, queue, ontarget, oninformation, onperiod }: Props = $props()
  const empty = $derived(emptyScope(projection))
</script>

{#if empty}
  <EmptyScope scope={empty} onjump={() => onperiod(empty.jump)} />
{:else}
  <FinanceTable
    {projection}
    cursorIndex={controller.cursorIndex}
    onmove={(delta) => void queue.run(() => controller.moveCursor(delta))}
    onhome={() => void queue.run(() => controller.moveHome())}
    onactivate={(identity, kind) => ontarget('view.drill', identity, kind)}
    onselect={(identity, kind) => ontarget('selection.toggle', identity, kind)}
    {oninformation}
  />
{/if}
