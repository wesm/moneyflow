<script lang="ts">
  import { StatusBar, ThemeToggle, Toggle, TopBar } from '@kenn-io/kit-ui'
  import { onMount, tick } from 'svelte'
  import FiltersDialog from './FiltersDialog.svelte'
  import FinanceTable from './FinanceTable.svelte'
  import HelpDialog from './HelpDialog.svelte'
  import RefinementBar from './RefinementBar.svelte'
  import SearchOverlay from './SearchOverlay.svelte'
  import { createMoneyflowShortcuts, type LocalAction } from '../lib/shortcuts'
  import type { ViewController } from '../lib/controller/view-controller.svelte'
  interface Props {
    controller: ViewController
  }
  let { controller }: Props = $props()
  let charts = $state(true)
  let grid = $state<HTMLElement | undefined>()
  let overlay = $state<'search' | 'filters' | 'help' | undefined>()
  let popOverlayScope: (() => void) | undefined
  let shortcuts: ReturnType<typeof createMoneyflowShortcuts> | undefined
  const projection = $derived(controller.projection)

  function local(action: LocalAction): void {
    if (action === 'cursor.up') void controller.moveCursor(-1)
    else if (action === 'cursor.down') void controller.moveCursor(1)
    else if (action === 'cursor.home') void controller.moveHome()
    else if (action === 'overlay.search') openOverlay('search')
    else if (action === 'overlay.filters') openOverlay('filters')
    else if (action === 'overlay.help') openOverlay('help')
  }
  async function apply(action: string): Promise<void> {
    if (action === 'view.drill' || action === 'selection.toggle') {
      const row = [...(projection?.detail_rows ?? []), ...(projection?.aggregate_rows ?? [])].find(
        (candidate) => candidate.index === controller.cursorIndex,
      )
      if (row) {
        await target(action, row.identity, projection?.detail_rows ? 'detail' : 'aggregate')
        return
      }
    }
    await controller.apply({ action })
    focusGrid()
  }
  async function target(
    action: string,
    identity: string,
    kind: 'detail' | 'aggregate',
  ): Promise<void> {
    await controller.apply({ action, target: { identity, kind } })
    focusGrid()
  }
  function openOverlay(next: 'search' | 'filters' | 'help'): void {
    popOverlayScope?.()
    popOverlayScope = shortcuts?.manager.pushScope(next)
    overlay = next
  }
  function closeOverlay(): void {
    overlay = undefined
    popOverlayScope?.()
    popOverlayScope = undefined
    void tick().then(focusGrid)
  }
  function focusGrid(): void {
    grid?.querySelector<HTMLElement>('[role="grid"]')?.focus()
  }
  onMount(() => {
    if (!projection) return
    shortcuts = createMoneyflowShortcuts(projection.capabilities ?? [], {
      local,
      apply: (action) => void apply(action),
    })
    const keydown = (event: KeyboardEvent) => shortcuts?.manager.handleKeydown(event)
    window.addEventListener('keydown', keydown)
    focusGrid()
    return () => {
      window.removeEventListener('keydown', keydown)
      popOverlayScope?.()
      shortcuts?.destroy()
    }
  })
</script>

{#if projection}
  <div class="app-shell">
    <TopBar ariaLabel="Moneyflow">
      {#snippet left()}<span class="moneyflow-brand">Moneyflow</span>{/snippet}
      {#snippet right()}<Toggle bind:checked={charts} label="Charts" /><ThemeToggle
          size="sm"
        />{/snippet}
    </TopBar>
    <RefinementBar {projection} onaction={(action) => void apply(action)} />
    <main class="app-shell__main" aria-label="Moneyflow workspace">
      <div class="app-shell__table" bind:this={grid}>
        <FinanceTable
          {projection}
          cursorIndex={controller.cursorIndex}
          onmove={(delta) => void controller.moveCursor(delta)}
          onhome={() => void controller.moveHome()}
          onactivate={(identity, kind) => void target('view.drill', identity, kind)}
          onselect={(identity, kind) => void target('selection.toggle', identity, kind)}
        />
      </div>
      {#if charts}<aside aria-label="Visualizations">Charts follow in the next slice.</aside>{/if}
    </main>
    <p class="kit-sr-only" aria-live="polite">{controller.announcement}</p>
    <StatusBar
      >{#snippet left()}<span>{projection.total_rows} results</span
        >{/snippet}{#snippet right()}<span>read only</span>{/snippet}</StatusBar
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
  {/if}
{/if}
