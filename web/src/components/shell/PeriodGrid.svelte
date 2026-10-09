<script lang="ts">
  import { gridStep, type PeriodCell } from '../../lib/shell/period'

  interface Props {
    label: string
    cells: PeriodCell[]
    columns: number
    /** Cells that take focus when none is selected, in order of preference. */
    starts: string[]
    onpick: (key: string) => void
  }

  let { label, cells, columns, starts, onpick }: Props = $props()
  let group = $state<HTMLElement>()
  let moved = $state<string | undefined>()
  // One tab stop: the cell last moved to, the selected cell, a start cell, or the first enabled.
  const focusIndex = $derived.by(() => {
    for (const wanted of [moved, cells.find((cell) => cell.selected)?.key, ...starts]) {
      const index = cells.findIndex((cell) => cell.key === wanted && !cell.disabled)
      if (index >= 0) return index
    }
    return Math.max(
      0,
      cells.findIndex((cell) => !cell.disabled),
    )
  })

  function keydown(event: KeyboardEvent): void {
    const disabled = cells.map((cell) => cell.disabled)
    const next = gridStep(event.key, focusIndex, disabled, columns)
    if (next === undefined) return
    event.preventDefault()
    moved = cells[next]?.key
    group?.querySelectorAll<HTMLButtonElement>('button')[next]?.focus()
  }
</script>

<div
  class="mf-period-grid"
  style:--mf-period-columns={columns}
  role="group"
  aria-label={label}
  bind:this={group}
>
  {#each cells as cell, index (cell.key)}
    <button
      type="button"
      class="mf-period-grid__cell"
      aria-label={cell.name}
      aria-current={cell.selected ? 'date' : undefined}
      tabindex={index === focusIndex ? 0 : -1}
      disabled={cell.disabled}
      onkeydown={keydown}
      onclick={() => onpick(cell.key)}
    >
      {cell.label}
    </button>
  {/each}
</div>
