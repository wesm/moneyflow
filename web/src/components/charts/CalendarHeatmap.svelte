<script lang="ts">
  import { Tooltip } from '@kenn-io/kit-ui'

  import { calendarCells, type CalendarCell } from '../../lib/charts/calendar'
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
  const response = $derived(entry.response)
  const heatmap = $derived(response.ranges.heatmap)
  const blocks = $derived.by(() => {
    const calendar = response.summary?.calendar
    if (heatmap === undefined || calendar === undefined) return []
    const days = { ...calendar, scale: response.scale }
    return calendarCells(heatmap, days, response.ranges.scope, response.today, cursorDate)
  })
  const weeks = $derived(heatmap?.layout === 'weeks')
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
  <p class="mf-panel__title" {id}>Daily spending</p>
  <div
    class={['mf-calendar', weeks && 'mf-calendar--weeks']}
    role="group"
    aria-labelledby={id}
    style:--mf-months={blocks.length}
    bind:this={grid}
  >
    {#each blocks as block, index (index)}
      <div class="mf-calendar__block">
        {#if block.label}
          <p class="mf-calendar__label">{block.label}</p>
        {/if}
        <div class="mf-calendar__days">
          {#each block.weeks.flat() as cell, slot (slot)}
            {#if cell === undefined}
              <span></span>
            {:else if cell.future}
              <span class="mf-day--future"></span>
            {:else}
              <Tooltip text={cell.name}>
                <button
                  type="button"
                  class={[
                    'mf-day',
                    `mf-day--l${cell.outLevel}`,
                    cell.hasIn && 'mf-day--in',
                    !cell.inScope && 'mf-day--outside',
                    cell.cursor && 'mf-day--cursor',
                  ]}
                  aria-label={cell.name}
                  tabindex={cell === active ? 0 : -1}
                  onclick={() => onperiod({ start: cell.date, end: cell.date })}
                  onkeydown={(event) => keydown(event, cell)}
                  onfocus={() => (chosen = { query: entry.query, date: cell.date })}
                ></button>
              </Tooltip>
            {/if}
          {/each}
        </div>
      </div>
    {/each}
  </div>
</div>
