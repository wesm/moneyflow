<script lang="ts">
  import { TextInput } from '@kenn-io/kit-ui'

  import { editorKey, type EditorKeyAction } from '../../lib/editing/editor-keys'
  import type { EditorOption } from '../../lib/editing/options'

  interface Props {
    /** Accessible name of the field and its list. */
    label: string
    /** The options for the current text, from `editorOptions`. */
    options: EditorOption[]
    /** The typed text (bindable). */
    value: string
    /** Whether the current text can be staged; Enter with the list closed asks only then. */
    valid: boolean
    /** Whether a plain Enter on the open list stages the choice it accepts; see `editorKey`. */
    acceptStages?: boolean
    /** Shows the list as soon as it has options, before any key. */
    startOpen?: boolean
    /** The value being changed. Its option is marked, and is highlighted while the field is empty. */
    current?: string | undefined
    /**
     * Highlights the first option as the list opens. Off, nothing is highlighted until an arrow
     * key, so Enter keeps the typed text.
     */
    highlightFirst?: boolean
    onaccept: (option: EditorOption) => void
    /**
     * Receives `stage`, `accept-and-stage` (after `onaccept`), and `cancel`, with the key event.
     * The cancelling Escape is not claimed here; a host that acts on it claims it.
     */
    onkeyaction: (action: EditorKeyAction, event: KeyboardEvent) => void
  }

  let {
    label,
    options,
    value = $bindable(),
    valid,
    acceptStages = false,
    startOpen = false,
    current,
    highlightFirst = true,
    onaccept,
    onkeyaction,
  }: Props = $props()

  const id = $props.id()
  const listId = `${id}-list`
  // kit types `inputEl` without `| undefined`, so the binding casts the unmounted field.
  let input = $state<HTMLInputElement>()
  let list = $state<HTMLElement>()
  // svelte-ignore state_referenced_locally
  let open = $state(startOpen)
  /** The option an arrow key moved to; until then, the default below. */
  let active = $state<number>()
  const currentIndex = $derived(
    current === undefined ? -1 : options.findIndex((option) => option.label === current),
  )
  const defaultIndex = $derived(
    value.trim() === '' && currentIndex >= 0 ? currentIndex : highlightFirst ? 0 : -1,
  )
  const activeIndex = $derived(Math.min(active ?? defaultIndex, options.length - 1))
  const expanded = $derived(open && options.length > 0)
  const activeOption = $derived(
    expanded && activeIndex >= 0 ? { ariaActivedescendant: `${listId}-${activeIndex}` } : {},
  )

  // A listener on the input itself runs before any ancestor's keydown listener, so a claimed
  // Escape is already default-prevented and stopped when the editor's overlay sees the event.
  $effect(() => {
    const element = input
    if (!element) return
    element.addEventListener('keydown', handleKey)
    return () => element.removeEventListener('keydown', handleKey)
  })

  $effect(() => {
    if (expanded && activeIndex >= 0) {
      list?.children[activeIndex]?.scrollIntoView({ block: 'nearest' })
    }
  })

  function handleKey(event: KeyboardEvent): void {
    let action = editorKey(event, {
      listOpen: expanded,
      highlighted: activeIndex >= 0,
      valid,
      focus: 'combobox',
      acceptStages,
    })
    // With nothing typed, the list is all there is to close, so Escape cancels at once.
    if (action === 'close-list' && value.trim() === '') action = 'cancel'
    if (action === 'none') return
    if (action === 'cancel') {
      onkeyaction(action, event)
      return
    }
    if (action === 'close-list') event.stopPropagation()
    if (action !== 'open' || event.key.startsWith('Arrow')) event.preventDefault()
    run(action, event)
  }

  function run(action: EditorKeyAction, event: KeyboardEvent): void {
    if (action === 'open') {
      reopen()
    } else if (action === 'move-next') {
      active = (activeIndex + 1) % options.length
    } else if (action === 'move-prev') {
      active =
        activeIndex < 0 ? options.length - 1 : (activeIndex - 1 + options.length) % options.length
    } else if (action === 'close-list') {
      open = false
    } else if (action === 'accept') {
      accept(options[activeIndex])
    } else {
      // A stage request closes the list, so choices that load later do not open it.
      if (action === 'accept-and-stage') accept(options[activeIndex])
      else open = false
      onkeyaction(action, event)
    }
  }

  function accept(option: EditorOption | undefined): void {
    open = false
    if (!option) return
    value = option.label
    onaccept(option)
  }

  function reopen(): void {
    open = true
    active = undefined
  }

  function optionName(option: EditorOption): string {
    if (option.create) return `Create "${option.label}"`
    const count = `${option.count} ${option.count === 1 ? 'transaction' : 'transactions'}`
    const mark = option.label === current ? 'current' : ''
    return [option.label, option.secondary, count, mark].filter(Boolean).join(', ')
  }
</script>

{#snippet optionRow(option: EditorOption, index: number)}
  <li
    id={`${listId}-${index}`}
    role="option"
    aria-selected={expanded && index === activeIndex}
    aria-label={optionName(option)}
    onmousedown={() => accept(option)}
  >
    {#if option.create}
      Create "{option.label}"
    {:else}
      <span class="mf-combobox-label">{option.label}</span>
      {#if option.secondary}<span class="mf-combobox-quiet">{option.secondary}</span>{/if}
      {#if option.label === current}<span class="mf-combobox-current">Current</span>{/if}
      <span class="mf-combobox-quiet mf-combobox-count">{option.count}</span>
    {/if}
  </li>
{/snippet}

<div class="mf-combobox" onfocusout={() => (open = false)}>
  <!-- prettier-ignore -->
  <TextInput
    bind:value
    bind:inputEl={() => input as HTMLInputElement, (element) => (input = element)}
    block
    ariaLabel={label}
    // kit-ui-check-ignore: kit Typeahead clears typed text on close and opens only from a button
    role="combobox"
    ariaExpanded={expanded}
    ariaControls={listId}
    ariaAutocomplete="list"
    {...activeOption}
    autocomplete="off"
    oninput={reopen}
  />
  <!-- A press on the list's padding or scrollbar must not blur the input and close it. -->
  <!-- prettier-ignore -->
  <ul
    bind:this={list}
    class="mf-combobox-list kit-popover-card"
    id={listId}
    // kit-ui-check-ignore: kit Typeahead clears typed text on close and opens only from a button
    role="listbox"
    aria-label={label}
    hidden={!expanded}
    onmousedown={(event) => event.preventDefault()}
  >
    {#each options as option, index (`${option.create}:${option.id}`)}
      {@render optionRow(option, index)}
    {/each}
  </ul>
</div>

<style>
  .mf-combobox {
    position: relative;
  }
  .mf-combobox-list {
    position: absolute;
    inset-inline: 0;
    top: calc(100% + var(--space-1));
    z-index: var(--z-popover);
    max-height: 240px;
    overflow-y: auto;
    margin: 0;
    padding: 2px;
    list-style: none;
  }
  li {
    display: flex;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }
  li:hover {
    background: color-mix(in srgb, var(--bg-surface-hover) 50%, transparent);
  }
  li[aria-selected='true'] {
    background: var(--bg-surface-hover);
    outline: 1px solid var(--accent-blue);
    outline-offset: -1px;
  }
  .mf-combobox-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mf-combobox-quiet {
    flex-shrink: 0;
    color: var(--text-secondary);
  }
  .mf-combobox-current {
    flex-shrink: 0;
    color: var(--accent-blue);
    font-size: var(--font-size-xs);
  }
  .mf-combobox-count {
    margin-left: auto;
    font-variant-numeric: tabular-nums;
  }
</style>
