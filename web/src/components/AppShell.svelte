<script lang="ts">
  import {
    Button,
    DetailDrawer,
    MEDIA,
    StatusBar,
    ThemeToggle,
    Toggle,
    TopBar,
  } from '@kenn-io/kit-ui'
  import { MediaQuery } from 'svelte/reactivity'
  import { onMount, tick, untrack } from 'svelte'
  import FiltersDialog from './FiltersDialog.svelte'
  import FinanceTable from './FinanceTable.svelte'
  import HelpDialog from './HelpDialog.svelte'
  import RefinementBar from './RefinementBar.svelte'
  import SearchOverlay from './SearchOverlay.svelte'
  import VisualizationRail from './VisualizationRail.svelte'
  import CategoryDialog from './editing/CategoryDialog.svelte'
  import CategoryManager from './editing/CategoryManager.svelte'
  import GroupManager from './editing/GroupManager.svelte'
  import MerchantDialog from './editing/MerchantDialog.svelte'
  import PendingStatus from './editing/PendingStatus.svelte'
  import DeleteConfirmation from './editing/DeleteConfirmation.svelte'
  import DuplicateReview from './editing/DuplicateReview.svelte'
  import ExportDialog from './editing/ExportDialog.svelte'
  import ProviderStatus from './ProviderStatus.svelte'
  import TransactionInformationDrawer from './TransactionInformationDrawer.svelte'
  import ReviewDrawer from './editing/ReviewDrawer.svelte'
  import WriteStatusDrawer from './editing/WriteStatusDrawer.svelte'
  import {
    createMoneyflowShortcuts,
    handleMoneyflowKeydown,
    type LocalAction,
  } from '../lib/shortcuts'
  import type { ViewController } from '../lib/controller/view-controller.svelte'
  import type { TransactionInformationResponse } from '../lib/api/client'
  interface Props {
    controller: ViewController
    onreconnect?: (() => void) | undefined
    onamazonimport?: (() => void) | undefined
  }
  let { controller, onreconnect, onamazonimport }: Props = $props()
  let charts = $state(true)
  let chartDrawer = $state(false)
  let grid = $state<HTMLElement | undefined>()
  type Overlay =
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
  let overlay = $state<Overlay | undefined>()
  let popOverlayScope: (() => void) | undefined
  let popChartScope: (() => void) | undefined
  let shortcuts: ReturnType<typeof createMoneyflowShortcuts> | undefined
  let focusRestoreFrame: number | undefined
  let deleteTarget = $state<{ identity: string; kind: 'transaction' } | undefined>()
  let deleteCount = $state(0)
  let transactionInformation = $state<TransactionInformationResponse | undefined>()
  let transitionTail: Promise<void> = Promise.resolve()
  const projection = $derived(controller.projection)
  const compact = new MediaQuery(MEDIA.compact)

  function local(action: LocalAction): void {
    if (action === 'cursor.up') void controller.moveCursor(-1)
    else if (action === 'cursor.down') void controller.moveCursor(1)
    else if (action === 'cursor.home') void controller.moveHome()
    else if (action === 'overlay.search') openOverlay('search')
    else if (action === 'overlay.filters') openOverlay('filters')
    else if (action === 'overlay.help') openOverlay('help')
    else if (action === 'edit.merchant') {
      if (focusedRow()) openOverlay('merchant')
    } else if (action === 'edit.category') {
      if (focusedRow()) openOverlay('category')
    } else if (action === 'manage.categories') openOverlay('categories')
    else if (action === 'manage.groups') openOverlay('groups')
    else if (action === 'edit.review') openOverlay('review')
    else if (action === 'view.duplicates') openOverlay('duplicates')
    else if (action === 'provider.refresh') {
      if (projection?.profile_kind === 'amazon') onamazonimport?.()
      else void controller.provider.refresh()
    } else if (action === 'transactions.export') void beginExport()
    else if (action === 'edit.undo') void controller.editing.undo()
    else if (action === 'edit.redo') void controller.editing.redo()
    else if (action === 'edit.hide') {
      const row = focusedRow()
      if (row) {
        void controller.editing.submit({
          action: 'transaction.toggle-hidden',
          input: {},
          target: row,
        })
      }
    } else if (action === 'edit.delete') {
      const row = focusedRow()
      if (row?.kind === 'transaction') {
        deleteTarget = { identity: row.identity, kind: 'transaction' }
        deleteCount = projection?.selection_count ? projection.selection_count : 1
        openOverlay('delete')
      }
    } else if (action === 'transaction.info') {
      void transitionTail.then(async () => {
        await tick()
        const row = focusedRow()
        if (row?.kind === 'transaction') await openTransactionInformation(row.identity)
      })
    }
  }
  async function beginExport(): Promise<void> {
    if (!projection) return
    if (await controller.export.open(projection.canonical_query)) openOverlay('export')
  }
  async function openTransactionInformation(identity: string): Promise<void> {
    const information = await controller.transactionInformation?.(identity)
    if (!information) return
    transactionInformation = information
    openOverlay('information')
  }
  function focusedRow(): { identity: string; kind: 'transaction' | 'aggregate' } | undefined {
    const row = [...(projection?.detail_rows ?? []), ...(projection?.aggregate_rows ?? [])].find(
      (candidate) => candidate.index === controller.cursorIndex,
    )
    return row
      ? { identity: row.identity, kind: projection?.detail_rows ? 'transaction' : 'aggregate' }
      : undefined
  }
  async function apply(action: string): Promise<void> {
    if (action === 'view.drill' || action === 'selection.toggle') {
      const row = [...(projection?.detail_rows ?? []), ...(projection?.aggregate_rows ?? [])].find(
        (candidate) => candidate.index === controller.cursorIndex,
      )
      if (row) {
        await target(action, row.identity, projection?.detail_rows ? 'transaction' : 'aggregate')
        return
      }
    }
    await controller.apply({ action })
    focusGrid()
  }
  function queueApply(action: string): void {
    const run = () => apply(action)
    transitionTail = transitionTail.then(run, run)
  }
  async function target(
    action: string,
    identity: string,
    kind: 'transaction' | 'aggregate',
  ): Promise<void> {
    await controller.apply({ action, target: { identity, kind } })
    focusGrid()
  }
  function openOverlay(next: Overlay): void {
    if (focusRestoreFrame !== undefined) cancelAnimationFrame(focusRestoreFrame)
    focusRestoreFrame = undefined
    popOverlayScope?.()
    popOverlayScope = shortcuts?.manager.pushScope(next)
    overlay = next
  }
  function closeOverlay(): void {
    overlay = undefined
    popOverlayScope?.()
    popOverlayScope = undefined
    deleteTarget = undefined
    deleteCount = 0
    transactionInformation = undefined
    void tick().then(() => {
      focusRestoreFrame = requestAnimationFrame(() => {
        focusRestoreFrame = undefined
        if (overlay === undefined) focusGrid()
      })
    })
  }
  function focusGrid(): void {
    grid?.querySelector<HTMLElement>('[role="grid"]')?.focus({ preventScroll: true })
  }
  async function confirmDirectDelete(): Promise<void> {
    if (!deleteTarget) return
    const accepted = await controller.editing.submit({
      action: 'transaction.delete',
      input: {},
      target: deleteTarget,
    })
    if (accepted) closeOverlay()
  }
  function toggleCharts(checked: boolean): void {
    if (compact.current) setChartDrawer(checked)
    else charts = checked
  }
  function setChartDrawer(open: boolean): void {
    popChartScope?.()
    popChartScope = open ? shortcuts?.manager.pushScope('charts') : undefined
    chartDrawer = open
  }
  $effect(() => {
    if (!compact.current && chartDrawer) setChartDrawer(false)
  })
  $effect(() => {
    if (!projection) return
    const current = createMoneyflowShortcuts(projection.capabilities ?? [], {
      local,
      apply: queueApply,
    })
    shortcuts = current
    const activeOverlay = untrack(() => overlay)
    const activeChartDrawer = untrack(() => chartDrawer)
    popOverlayScope = activeOverlay ? current.manager.pushScope(activeOverlay) : undefined
    popChartScope = activeChartDrawer ? current.manager.pushScope('charts') : undefined
    return () => current.destroy()
  })
  $effect(() => {
    if (controller.provider.state.phase !== 'confirmation') return
    const popScope = shortcuts?.manager.pushScope('provider-confirmation')
    return () => popScope?.()
  })
  $effect(() => {
    if (controller.providerWrite.state.phase === 'confirmation') openOverlay('write')
  })
  onMount(() => {
    if (!projection) return
    const keydown = (event: KeyboardEvent) =>
      shortcuts ? handleMoneyflowKeydown(shortcuts.manager, event) : false
    window.addEventListener('keydown', keydown)
    focusGrid()
    return () => {
      window.removeEventListener('keydown', keydown)
      popOverlayScope?.()
      popChartScope?.()
      if (focusRestoreFrame !== undefined) cancelAnimationFrame(focusRestoreFrame)
    }
  })
</script>

{#if projection}
  <div class="app-shell">
    <TopBar ariaLabel="Moneyflow">
      {#snippet left()}<span class="moneyflow-brand">Moneyflow</span>{/snippet}
      {#snippet right()}{#if projection.profile_kind === 'amazon'}<Button
            size="sm"
            onclick={onamazonimport}>Import Amazon orders</Button
          >{:else}<ProviderStatus
            controller={controller.provider}
            {onreconnect}
          />{/if}{#if controller.providerWrite.state.status?.phase}<Button
            size="sm"
            onclick={() => openOverlay('write')}
            >Write {controller.providerWrite.state.status.completed}/{controller.providerWrite.state
              .status.total}</Button
          >{/if}<Toggle
          checked={compact.current ? chartDrawer : charts}
          onchange={toggleCharts}
          label="Charts"
        /><ThemeToggle size="sm" />{/snippet}
    </TopBar>
    <RefinementBar {projection} onaction={(action) => void apply(action)} />
    <main class="app-shell__main" aria-label="Moneyflow workspace">
      <h1 class="kit-sr-only">Moneyflow transaction workspace</h1>
      <div class="app-shell__table" bind:this={grid}>
        <FinanceTable
          {projection}
          cursorIndex={controller.cursorIndex}
          onmove={(delta) => void controller.moveCursor(delta)}
          onhome={() => void controller.moveHome()}
          onactivate={(identity, kind) => void target('view.drill', identity, kind)}
          onselect={(identity, kind) => void target('selection.toggle', identity, kind)}
          oninformation={(identity) => void openTransactionInformation(identity)}
        />
      </div>
      {#if charts && !compact.current}
        <aside aria-label="Visualizations">
          <VisualizationRail
            {projection}
            cursorIndex={controller.cursorIndex}
            oncursor={(index) => void controller.moveCursorTo(index)}
            ondrill={(identity) => void target('view.drill', identity, 'aggregate')}
          />
        </aside>
      {/if}
    </main>
    <p class="kit-sr-only" aria-live="polite">{controller.announcement}</p>
    <p class="kit-sr-only" aria-live="polite">{controller.editing.state.announcement}</p>
    <p class="kit-sr-only" aria-live="polite">{controller.provider.state.announcement}</p>
    <p class="kit-sr-only" aria-live="polite">{controller.providerWrite.state.announcement}</p>
    <p class="kit-sr-only" aria-live="polite">{controller.duplicates.state.announcement}</p>
    <p class="kit-sr-only" aria-live="polite">{controller.export.state.announcement}</p>
    <StatusBar
      >{#snippet left()}<span
          >{projection.total_rows} results · <PendingStatus
            pending={controller.editing.state.pending}
          /></span
        >{/snippet}{#snippet right()}<span
          >profile revision {controller.editing.state.revision.toString(10)}</span
        >{/snippet}</StatusBar
    >
  </div>
  {#if overlay === 'search'}
    <SearchOverlay {controller} onclose={closeOverlay} />
  {:else if overlay === 'filters'}
    <FiltersDialog
      {projection}
      onapply={(action) => controller.apply(action)}
      onclose={closeOverlay}
    />
  {:else if overlay === 'help'}
    <HelpDialog capabilities={projection.capabilities ?? []} onclose={closeOverlay} />
  {:else if overlay === 'merchant' && focusedRow()}
    <MerchantDialog
      controller={controller.editing}
      target={focusedRow()!}
      hasSelection={projection.selection_count > 0}
      onclose={closeOverlay}
    />
  {:else if overlay === 'category' && focusedRow()}
    <CategoryDialog controller={controller.editing} target={focusedRow()!} onclose={closeOverlay} />
  {:else if overlay === 'categories'}
    <CategoryManager controller={controller.editing} onclose={closeOverlay} />
  {:else if overlay === 'groups'}
    <GroupManager controller={controller.editing} onclose={closeOverlay} />
  {:else if overlay === 'review'}
    <ReviewDrawer
      editing={controller.editing}
      review={controller.review}
      onclose={closeOverlay}
      onwrite={() => openOverlay('write')}
    />
  {:else if overlay === 'write'}
    <WriteStatusDrawer controller={controller.providerWrite} onclose={closeOverlay} {onreconnect} />
  {:else if overlay === 'duplicates'}
    <DuplicateReview controller={controller.duplicates} onclose={closeOverlay} />
  {:else if overlay === 'delete' && deleteTarget}
    <DeleteConfirmation
      count={deleteCount}
      submitting={controller.editing.state.phase === 'submitting'}
      onconfirm={() => void confirmDirectDelete()}
      oncancel={closeOverlay}
    />
  {:else if overlay === 'export'}
    <ExportDialog controller={controller.export} onclose={closeOverlay} />
  {:else if overlay === 'information' && transactionInformation}
    <TransactionInformationDrawer information={transactionInformation} onclose={closeOverlay} />
  {/if}
  {#if compact.current && chartDrawer}
    <DetailDrawer
      title="Visualizations"
      ariaLabel="Moneyflow visualizations"
      width="min(560px, calc(100vw - 1px))"
      onclose={() => setChartDrawer(false)}
    >
      <VisualizationRail
        {projection}
        cursorIndex={controller.cursorIndex}
        oncursor={(index) => void controller.moveCursorTo(index)}
        ondrill={(identity) => void target('view.drill', identity, 'aggregate')}
      />
    </DetailDrawer>
  {/if}
{/if}
