<script lang="ts">
  import { MEDIA } from '@kenn-io/kit-ui'
  import { untrack } from 'svelte'
  import { MediaQuery } from 'svelte/reactivity'

  import ChartArea from '../charts/ChartArea.svelte'
  import FinanceGrid from '../table/FinanceGrid.svelte'
  import RowActionsSheet from '../table/RowActionsSheet.svelte'
  import SelectionBar from '../table/SelectionBar.svelte'
  import ChartDrawer from './ChartDrawer.svelte'
  import EmptyScope from './EmptyScope.svelte'
  import type { ViewProjection } from '../../lib/api/client'
  import type { components } from '../../lib/api/schema'
  import { moveToRow } from '../../lib/charts/composition'
  import type {
    TransitionAction,
    ViewController,
  } from '../../lib/controller/view-controller.svelte'
  import { InsightsCache } from '../../lib/insights/cache.svelte'
  import { seriesRows } from '../../lib/insights/requests'
  import type { ChartState } from '../../lib/shell/chart-state.svelte'
  import {
    localActions,
    moveCursor,
    openEditorWhenSettled,
    openTransactionInformation,
    runRowAction,
    type LocalActionContext,
  } from '../../lib/shell/local-actions'
  import { emptyScope, type Period } from '../../lib/shell/period'
  import { status } from '../../lib/shell/status.svelte'
  import type { SelectionButton } from '../../lib/table/selection'
  import { provideRevealHold, RevealHold } from '../../lib/table/reveal-hold.svelte'
  import { tableInsights } from '../../lib/table/trend'

  interface Props {
    projection: ViewProjection
    actions: LocalActionContext
    /** Applies a view transition from the table, then returns focus to it. */
    onapply: (request: TransitionAction) => void
    charts: ChartState
  }

  let { projection, actions, onapply, charts }: Props = $props()
  const controller = $derived(actions.controller)
  // One cache per controller, and so per profile.
  const cache = $derived(insightsCache(controller))
  const insights = $derived(tableInsights(cache, projection.canonical_query))
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
  /** The measured height of the charts above the table. */
  let above = $state(0)
  // The KPI strip shows above phone width; on phones only the chart drawer reads insights.
  const charting = $derived(!compact.current || charts.dialogOpen)
  provideRevealHold(new RevealHold())
  /** The row whose ⋯ button opened the actions sheet. */
  let sheetRow = $state<components['schemas']['DetailRow']>()

  // The ⋯ buttons exist only on phone rows, so growing past 640 px closes their sheet.
  $effect(() => {
    if (!compact.current && untrack(() => actions.overlays.current) === 'actions') {
      actions.overlays.close()
    }
  })

  $effect(() => {
    const current = cache
    return () => current.dispose()
  })

  $effect(() => {
    syncInsights()
  })

  /** Syncs the charts' insights; an old answer makes the cache check the server's day again. */
  function syncInsights(): void {
    if (charting) cache.sync(projection, seriesRows(projection, controller.neighbors))
  }

  /** A tab in the background overnight has no new revision, so coming back syncs on its own. */
  function foreground(): void {
    if (document.visibilityState === 'visible') syncInsights()
  }

  function insightsCache(owner: ViewController): InsightsCache {
    return new InsightsCache(
      (body, signal) => owner.insights(body, signal),
      (revision) => actions.queue(() => owner.recheckNewer(revision)),
      () => actions.queue(() => owner.reloadDay()),
    )
  }

  function onperiod(period: Period): void {
    onapply({ action: 'time.set-period', period })
  }

  function onrow(identity: string): void {
    // The row is found in the projection the queued transitions leave.
    void moveCursor(actions, async () => {
      if (controller.projection) await moveToRow(controller, controller.projection, identity)
    })
  }

  function ondrill(identity: string): void {
    onapply({ action: 'view.drill', target: { identity, kind: 'aggregate' } })
  }

  /** Runs a selection bar button as its key does, then leaves focus on the table. */
  function select(action: SelectionButton['action']): void {
    status.clear()
    if (action === 'clear') {
      if (clearing) return
      clearing = true
      const settle = () => (clearing = false)
      return void actions.queue(clearSelection).then(settle, settle)
    }
    actions.focusGrid()
    local[action]()
  }

  // Back clears a selection. A toggle queued before the Clear can leave none, and Back would
  // then leave the search, sub-grouping, or drill level instead.
  async function clearSelection(): Promise<void> {
    if (controller.projection?.selection_count) await controller.apply({ action: 'view.back' })
    actions.focusGrid()
  }
</script>

<svelte:window onfocus={syncInsights} />
<svelte:document onvisibilitychange={foreground} />

<!-- The workspace's one scroll surface; the table header sticks to its top. -->
<div
  class={['mf-workspace', selecting && 'mf-workspace--selecting']}
  style:--mf-selection-height={`${covered}px`}
  bind:this={scroller}
  bind:clientHeight={viewport}
  onscroll={(event) => (scrollTop = event.currentTarget.scrollTop)}
>
  {#if !compact.current}
    <ChartArea
      {projection}
      {cache}
      chartsOn={charts.row}
      cursorIndex={controller.cursorIndex}
      {onperiod}
      {onrow}
      {ondrill}
      onleave={actions.focusGrid}
      measure={(height) => (above = height)}
    />
  {/if}
  {#if empty}
    <EmptyScope
      scope={empty}
      onjump={() => onapply({ action: 'time.set-period', period: empty.jump })}
    />
  {:else}
    <FinanceGrid
      {projection}
      neighbors={controller.neighbors}
      {insights}
      cursorIndex={controller.cursorIndex}
      {scroller}
      bind:scrollTop
      viewport={viewport - covered}
      {above}
      compact={compact.current}
      onmove={(delta) => moveCursor(actions, () => controller.moveCursor(delta))}
      onhome={() => moveCursor(actions, () => controller.moveHome())}
      oncursor={(index) => moveCursor(actions, () => controller.moveCursorTo(index))}
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
<ChartDrawer open={compact.current && charts.dialogOpen} onclose={() => charts.setDialog(false)}>
  <ChartArea
    {projection}
    {cache}
    chartsOn
    cursorIndex={controller.cursorIndex}
    {onperiod}
    {onrow}
    {ondrill}
  />
</ChartDrawer>
