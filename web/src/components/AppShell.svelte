<script lang="ts">
  import { StatusBar, ThemeToggle, Toggle, TopBar } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'
  import FinanceTable from './FinanceTable.svelte'
  import RefinementBar from './RefinementBar.svelte'
  import { createMoneyflowShortcuts, type LocalAction } from '../lib/shortcuts'
  import type { ViewController } from '../lib/controller/view-controller.svelte'
  interface Props {
    controller: ViewController
  }
  let { controller }: Props = $props()
  let charts = $state(true)
  let grid = $state<HTMLElement | undefined>()
  const projection = $derived(controller.projection)

  function local(action: LocalAction): void {
    if (action === 'cursor.up') void controller.moveCursor(-1)
    else if (action === 'cursor.down') void controller.moveCursor(1)
    else if (action === 'cursor.home') void controller.moveHome()
  }
  function apply(action: string): void {
    if (action === 'view.drill' || action === 'selection.toggle') {
      const row = [...(projection?.detail_rows ?? []), ...(projection?.aggregate_rows ?? [])].find(
        (candidate) => candidate.index === controller.cursorIndex,
      )
      if (row) {
        target(action, row.identity, projection?.detail_rows ? 'detail' : 'aggregate')
        return
      }
    }
    void controller.apply({ action })
  }
  function target(action: string, identity: string, kind: 'detail' | 'aggregate'): void {
    void controller.apply({ action, target: { identity, kind } })
  }
  onMount(() => {
    if (!projection) return
    const shortcuts = createMoneyflowShortcuts(projection.capabilities ?? [], { local, apply })
    const keydown = (event: KeyboardEvent) => shortcuts.manager.handleKeydown(event)
    window.addEventListener('keydown', keydown)
    grid?.querySelector<HTMLElement>('[role="grid"]')?.focus()
    return () => {
      window.removeEventListener('keydown', keydown)
      shortcuts.destroy()
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
    <RefinementBar {projection} onaction={apply} />
    <main class="app-shell__main" aria-label="Moneyflow workspace">
      <div class="app-shell__table" bind:this={grid}>
        <FinanceTable
          {projection}
          cursorIndex={controller.cursorIndex}
          onmove={(delta) => void controller.moveCursor(delta)}
          onhome={() => void controller.moveHome()}
          onactivate={(identity, kind) => target('view.drill', identity, kind)}
          onselect={(identity, kind) => target('selection.toggle', identity, kind)}
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
{/if}
