<script lang="ts">
  import { listMove, rowName, type ManagerRow } from '../../lib/editing/manager'

  interface Props {
    label: string
    rows: ManagerRow[]
    /** The active row's index in `rows`. */
    activeIndex: number
    /** The active row's ID (bindable); arrows, Home, End, and a press change it. */
    activeID: string
    /** The list element (bindable), which takes focus after a step. */
    element?: HTMLElement | undefined
    /** Up from the first row, which returns focus to the filter. */
    onleave: () => void
  }

  let {
    label,
    rows,
    activeIndex,
    // eslint-disable-next-line no-useless-assignment -- Writes update the parent's bind:activeID.
    activeID = $bindable(),
    element = $bindable(),
    onleave,
  }: Props = $props()
  const id = $props.id()

  $effect(() => {
    element?.children[activeIndex]?.scrollIntoView({ block: 'nearest' })
  })

  function keydown(event: KeyboardEvent): void {
    const next = listMove(event, activeIndex, rows.length)
    if (next === undefined) return
    event.preventDefault()
    if (next < 0) onleave()
    else activeID = rows[next]?.id ?? ''
  }
</script>

<!-- A press selects the row and leaves focus on the list. -->
<!-- prettier-ignore -->
<ul
  bind:this={element}
  class="mf-manager-list"
  // kit-ui-check-ignore: kit has no persistent single-select list; its listboxes are popups
  role="listbox"
  tabindex="0"
  aria-label={label}
  aria-activedescendant={rows.length ? `${id}-${activeIndex}` : undefined}
  onkeydown={keydown}
>
  {#each rows as row, index (row.id)}
    <li
      id={`${id}-${index}`}
      role="option"
      aria-selected={index === activeIndex}
      aria-label={rowName(row)}
      onmousedown={() => (activeID = row.id)}
    >
      <span>{row.label}</span>
      {#if row.secondary}<span class="mf-quiet">{row.secondary}</span>{/if}
      {#if row.protected}<span class="mf-quiet">Protected</span>{/if}
      <span class="mf-quiet mf-manager-list__count">{row.count}</span>
    </li>
  {/each}
</ul>

<style>
  /* The list takes the drawer's remaining height and scrolls, keeping the filter in view. */
  .mf-manager-list {
    flex: 1 1 0;
    min-height: 0;
    overflow-y: auto;
    margin: 0;
    padding: 0;
    list-style: none;
    outline: none;
  }
  li {
    display: flex;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  li > :first-child {
    width: 40%;
  }
  li[aria-selected='true'] {
    background: var(--bg-surface-hover);
  }
  .mf-manager-list:focus-visible li[aria-selected='true'] {
    outline: 1px solid var(--accent-blue);
    outline-offset: -1px;
  }
  @media (max-width: 640px) {
    li {
      min-height: 44px;
      align-items: center;
    }
  }
  .mf-manager-list__count {
    margin-left: auto;
    font-variant-numeric: tabular-nums;
  }
</style>
