<script lang="ts">
  import FinanceGrid from '../table/FinanceGrid.svelte'
  import EmptyScope from './EmptyScope.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { ViewController } from '../../lib/controller/view-controller.svelte'
  import type { FocusedRow } from '../../lib/shell/local-actions'
  import { emptyScope, type Period } from '../../lib/shell/period'

  interface Props {
    controller: ViewController
    projection: ViewProjection
    /** Sends a row action such as a drill or a selection toggle. */
    ontarget: (action: string, identity: string, kind: FocusedRow['kind']) => void
    oninformation: (identity: string) => void
    onperiod: (period: Period) => void
  }

  let { controller, projection, ontarget, oninformation, onperiod }: Props = $props()
  const empty = $derived(emptyScope(projection))
</script>

{#if empty}
  <EmptyScope scope={empty} onjump={() => onperiod(empty.jump)} />
{:else}
  <FinanceGrid
    {projection}
    cursorIndex={controller.cursorIndex}
    compact={false}
    onmove={(delta) => void controller.moveCursor(delta)}
    onhome={() => void controller.moveHome()}
    onactivate={(identity, kind) => ontarget('view.drill', identity, kind)}
    ontoggle={(identity, kind) => ontarget('selection.toggle', identity, kind)}
    {oninformation}
  />
{/if}
