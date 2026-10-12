<script lang="ts">
  import { EmptyState, SegmentedControl } from '@kenn-io/kit-ui'

  import {
    compositionBars,
    type Composition,
    type CompositionBar,
    type CompositionSide,
  } from '../../lib/charts/composition'
  import { revealHold } from '../../lib/table/reveal-hold.svelte'

  interface Props {
    /** Names the panel and its list, such as "Where it went". */
    title: string
    composition: Composition
    scale: number
    /** The cursor row's identity; its bar is drawn solid. */
    cursorIdentity?: string | undefined
    /** Moves the table cursor to a bar's row. Without it, the bars are not interactive. */
    onrow?: ((identity: string) => void) | undefined
    /** Drills into a bar's row, through the transition queue. */
    ondrill?: ((identity: string) => void) | undefined
    /** Esc leaves the panel for the table. */
    onleave?: (() => void) | undefined
  }

  let { title, composition, scale, cursorIdentity, onrow, ondrill, onleave }: Props = $props()
  const sides = [
    { value: 'spending', label: 'Spending' },
    { value: 'income', label: 'Income' },
  ]
  /** The switch only changes which ranking shows; both arrived with the summary. */
  let side = $state<CompositionSide>('spending')
  let panel = $state<HTMLElement>()
  let list = $state<HTMLElement>()
  const bars = $derived(compositionBars(composition, side, scale))
  const rows = $derived(onrow ? bars.filter((bar) => bar.identity !== undefined) : [])
  /** The bar that holds the Tab stop, while it is listed. */
  let chosen = $state<string>()
  const active = $derived(
    rows.find((bar) => bar.identity === chosen) ??
      rows.find((bar) => bar.identity === cursorIdentity) ??
      rows[0],
  )

  /** Where each key moves the Tab stop, from the bar at `at` with `last` the final bar. */
  const moves: Record<string, (at: number, last: number) => number> = {
    ArrowUp: (at) => at - 1,
    ArrowDown: (at) => at + 1,
    Home: () => 0,
    End: (_, last) => last,
  }

  // Esc anywhere in the panel, the switch included, returns to the table. It is claimed, so the
  // shell's Esc does not also go back a level.
  $effect(() => {
    const element = panel
    const leave = onleave
    if (!element || !leave) return
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      leave()
    }
    element.addEventListener('keydown', escape)
    return () => element.removeEventListener('keydown', escape)
  })

  // Revealing the cursor row can scroll the workspace and the chart with it. A pointer click
  // moves the cursor at once but holds the scroll, so a double-click's second click still lands
  // on this bar; that click drops the scroll, and the double-click drills.
  const doubleClickMillis = 400
  const hold = revealHold()
  $effect(() => () => hold?.end(false))

  function click(event: MouseEvent, identity: string): void {
    if (event.detail > 1) return hold?.end(false)
    // Space on the focused bar clicks with no count; its row is revealed at once.
    if (event.detail === 1) hold?.start(doubleClickMillis)
    else hold?.end(true)
    onrow?.(identity)
  }

  function keydown(event: KeyboardEvent, bar: CompositionBar, identity: string): void {
    // Enter drills, as it does on a table row; Space moves the cursor, as a click does.
    if (event.key === 'Enter') {
      event.preventDefault()
      ondrill?.(identity)
      return
    }
    const move = moves[event.key]
    if (move === undefined) return
    event.preventDefault()
    const next = Math.min(Math.max(move(rows.indexOf(bar), rows.length - 1), 0), rows.length - 1)
    chosen = rows[next]?.identity
    list?.querySelectorAll<HTMLElement>('button.mf-bar')[next]?.focus()
  }
</script>

<div class="mf-panel" bind:this={panel}>
  <div class="mf-panel__head">
    <p class="mf-panel__title">{title}</p>
    <SegmentedControl
      ariaLabel="Composition"
      options={sides}
      value={side}
      onchange={(value) => (side = value as CompositionSide)}
    />
  </div>
  {#if bars.length === 0}
    <EmptyState title={`No ${side}`} />
  {:else}
    <ul class={['mf-bars', `mf-bars--${side}`]} aria-label={title} bind:this={list}>
      {#each bars as bar (bar.key)}
        <li>
          {#if onrow && bar.identity !== undefined}
            {@const identity = bar.identity}
            <button
              type="button"
              class={['mf-bar', identity === cursorIdentity && 'mf-bar--cursor']}
              aria-label={bar.name}
              tabindex={bar === active ? 0 : -1}
              onclick={(event) => click(event, identity)}
              ondblclick={() => ondrill?.(identity)}
              onkeydown={(event) => keydown(event, bar, identity)}
              onfocus={() => (chosen = identity)}
            >
              {@render content(bar)}
            </button>
          {:else}
            <div class={['mf-bar', bar.others && 'mf-bar--others']}>{@render content(bar)}</div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

{#snippet content(bar: CompositionBar)}
  <span class="mf-bar__label">{bar.label}</span>
  <span class="mf-bar__track" aria-hidden="true">
    <span class="mf-bar__fill" style:width={`${bar.length / 100}%`}></span>
  </span>
  <span class="mf-bar__net">{bar.net}</span>
  <span class="mf-bar__share">{bar.share}</span>
{/snippet}
