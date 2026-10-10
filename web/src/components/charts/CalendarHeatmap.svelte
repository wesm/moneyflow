<script lang="ts">
  import { Tooltip } from '@kenn-io/kit-ui'

  import { calendarCells, type CalendarCell, type CalendarWeek } from '../../lib/charts/calendar'
  import type { InsightsEntry } from '../../lib/insights/cache.svelte'
  import type { Period } from '../../lib/shell/period'

  interface Props {
    entry: InsightsEntry
    /** The cursor transaction's date; that day is outlined. */
    cursorDate?: string | undefined
    /** Scopes the view to a clicked day. */
    onperiod: (period: Period) => void
    /** Esc leaves the heatmap for the table. */
    onleave?: (() => void) | undefined
  }

  let { entry, cursorDate, onperiod, onleave }: Props = $props()
  const id = $props.id()
  let grid = $state<HTMLElement>()
  let ticksRow = $state<HTMLElement>()
  /** The last month's name, ended at the strip's edge, would cover the name before it. */
  let crowded = $state(false)
  const response = $derived(entry.response)
  const heatmap = $derived(response.ranges.heatmap)
  const blocks = $derived.by(() => {
    const calendar = response.summary?.calendar
    if (heatmap === undefined || calendar === undefined) return []
    const days = { ...calendar, scale: response.scale }
    return calendarCells(heatmap, days, response.ranges.scope, response.today, cursorDate)
  })
  const weeks = $derived(heatmap?.layout === 'weeks')
  // In a Day scope the scoped day is outlined, so it stands out among the faded days.
  const dayScope = $derived(response.ranges.kind === 'day')
  /** The days a click or a key can reach, in date order: every day up to today. */
  const days = $derived(
    blocks
      .flatMap((block) => block.weeks.flat())
      .filter((cell): cell is CalendarCell => cell !== undefined && !cell.future),
  )
  /** The day that holds the Tab stop, while the same view is drawn. */
  let chosen = $state<{ query: string; date: string }>()
  const active = $derived(
    days.find((cell) => chosen?.query === entry.query && cell.date === chosen.date) ??
      days.find((cell) => cell.cursor) ??
      days.findLast((cell) => cell.inScope) ??
      days.at(-1),
  )
  /** A step along the days: a neighbor in its week, or the same weekday in the next week. */
  const steps = $derived<Record<string, number>>(
    weeks
      ? { ArrowUp: -1, ArrowDown: 1, ArrowLeft: -7, ArrowRight: 7 }
      : { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 },
  )

  /** The strip's month names as text, so a cursor move, which rebuilds the cells, keeps it. */
  const tickLabels = $derived(
    blocks
      .flatMap((block) => block.ticks ?? [])
      .map((tick) => `${tick.column}:${tick.label}`)
      .join(' '),
  )

  $effect(() => {
    const row = ticksRow
    if (!row || tickLabels === '') return
    const check = () => (crowded = overlapping(row))
    check()
    const observer = new ResizeObserver(check)
    observer.observe(row)
    return () => observer.disconnect()
  })

  function overlapping(row: HTMLElement): boolean {
    const [before, last] = [...row.children].slice(-2).map((tick) => tick.getBoundingClientRect())
    return before !== undefined && last !== undefined && last.left < before.right
  }

  function keydown(event: KeyboardEvent, cell: CalendarCell): void {
    if (event.key === 'Escape' && onleave) {
      event.preventDefault()
      onleave()
      return
    }
    const at = days.indexOf(cell)
    const last = days.length - 1
    const step = steps[event.key]
    const target = { Home: 0, End: last }[event.key] ?? (step === undefined ? undefined : at + step)
    if (target === undefined) return
    event.preventDefault()
    const next = Math.min(Math.max(target, 0), last)
    chosen = { query: entry.query, date: days[next]?.date ?? cell.date }
    grid?.querySelectorAll<HTMLElement>('button.mf-day')[next]?.focus()
  }
</script>

<div class="mf-panel">
  <div class="mf-panel__head">
    <p class="mf-panel__title" {id}>Daily spending</p>
    <!-- Levels rank the days with Out, so the key shows order, not amounts. -->
    <p class="mf-legend" aria-hidden="true">
      <span>Less</span>
      {#each [0, 1, 2, 3, 4] as level (level)}
        <span
          class={['mf-legend__swatch', level === 0 && 'mf-heat--empty']}
          style:--mf-level={level}
        ></span>
      {/each}
      <span>More</span>
      <span class="mf-legend__in">In</span>
    </p>
  </div>
  <div
    class={['mf-calendar', weeks && 'mf-calendar--weeks']}
    role="group"
    aria-labelledby={id}
    style:--mf-weeks={blocks[0]?.weeks.length}
    bind:this={grid}
  >
    {#each blocks as block, index (index)}
      {#if block.ticks}
        <div class="mf-calendar__ticks" aria-hidden="true" bind:this={ticksRow}>
          {#each block.ticks as tick (tick.column)}
            <span
              class={[
                tick.end && 'mf-calendar__tick--end',
                tick.end && crowded && 'mf-calendar__tick--hidden',
              ]}
              style:grid-column-start={tick.column + 1}>{tick.label}</span
            >
          {/each}
        </div>
        <div class="mf-calendar__rows" aria-hidden="true">
          {#each ['', 'M', '', 'W', '', 'F', ''] as initial, row (row)}
            <span class="mf-calendar__weekday">{initial}</span>
          {/each}
        </div>
        {@render calendarDays(block.weeks)}
      {:else}
        <div class="mf-calendar__month">
          <p class="mf-calendar__label">{block.label}</p>
          {@render calendarDays(block.weeks, true)}
        </div>
      {/if}
    {/each}
  </div>
</div>

{#snippet calendarDays(slots: CalendarWeek[], month = false)}
  <div class="mf-calendar__days">
    {#if month}
      {#each ['S', 'M', 'T', 'W', 'T', 'F', 'S'] as initial, column (column)}
        <span class="mf-calendar__weekday" aria-hidden="true">{initial}</span>
      {/each}
    {/if}
    {#each slots.flat() as cell, slot (slot)}
      {#if cell === undefined}
        <span></span>
      {:else if cell.future}
        <span class="mf-day--future"></span>
      {:else}
        <Tooltip>
          {#snippet content()}
            <p class="mf-cashflow-tip__title">{cell.label}</p>
            <dl class="mf-cashflow-tip">
              <dt>Out</dt>
              <dd>{cell.out}</dd>
              <dt>In</dt>
              <dd>{cell.in}</dd>
            </dl>
          {/snippet}
          <button
            type="button"
            class={[
              'mf-day',
              cell.outLevel === 0 && 'mf-heat--empty',
              cell.hasIn && 'mf-day--in',
              !cell.inScope && 'mf-day--outside',
              dayScope && cell.inScope && 'mf-day--scope',
              cell.cursor && 'mf-day--cursor',
            ]}
            style:--mf-level={cell.outLevel}
            aria-label={cell.name}
            tabindex={cell === active ? 0 : -1}
            onclick={() => onperiod({ start: cell.date, end: cell.date })}
            onkeydown={(event) => keydown(event, cell)}
            onfocus={() => (chosen = { query: entry.query, date: cell.date })}
          >
            {#if month}{cell.day}{/if}
          </button>
        </Tooltip>
      {/if}
    {/each}
  </div>
{/snippet}
