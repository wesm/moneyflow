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
      <span class="mf-manager-list__label">{row.label}</span>
      {#if row.secondary}<span class="mf-quiet mf-manager-list__secondary">{row.secondary}</span>{/if}
      {#if row.protected}<span class="mf-manager-list__badge">Protected</span>{/if}
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
  /* Name, then the group a category belongs to, then a Protected mark, then the count: each
     sits in its own column, so a row without one of them leaves the others aligned. */
  li {
    display: grid;
    grid-template-columns: minmax(0, 3fr) minmax(0, 2fr) 5.5rem 3.5rem;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .mf-manager-list__label,
  .mf-manager-list__secondary {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mf-manager-list__label {
    grid-column: 1;
  }
  .mf-manager-list__secondary {
    grid-column: 2;
  }
  .mf-manager-list__badge {
    grid-column: 3;
    justify-self: start;
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--border-default);
    border-radius: 999px;
    color: var(--text-muted);
    font-size: var(--font-size-xs);
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
      box-sizing: border-box;
      min-height: 44px;
      align-items: center;
    }
  }
  .mf-manager-list__count {
    grid-column: 4;
    text-align: end;
    font-variant-numeric: tabular-nums;
  }
</style>
