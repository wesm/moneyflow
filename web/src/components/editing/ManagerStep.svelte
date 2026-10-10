<script lang="ts">
  import { Button, Card, KbdBadge, SelectDropdown, TextInput } from '@kenn-io/kit-ui'

  import EditorCombobox from './EditorCombobox.svelte'
  import { matchChoice } from '../../lib/editing/editor-copy'
  import type { EditorKeyAction } from '../../lib/editing/editor-keys'
  import {
    CONFIRM_LABELS,
    pickerLabel,
    plainEnter,
    stepNote,
    stepTitle,
    type ManagerKind,
    type ManagerStep,
  } from '../../lib/editing/manager'
  import { editorOptions, type EditorChoice } from '../../lib/editing/options'
  import { transactionCount } from '../../lib/editing/toasts'

  interface Props {
    kind: ManagerKind
    /** The open step (bindable); its name, group, and destination fields change here. */
    step: ManagerStep
    /** The groups a new category or a move may use, or the items a merge or delete may use. */
    choices: EditorChoice[]
    /** Catalog groups, which label each category in the destination list. */
    groups: EditorChoice[]
    /** Whether the step has what its action needs and is not already staging. */
    ready: boolean
    error: string
    onconfirm: () => void
    onback: () => void
  }

  let {
    kind,
    step = $bindable(),
    choices,
    groups,
    ready,
    error,
    onconfirm,
    onback,
  }: Props = $props()
  const action = $derived(step.action)
  const named = $derived(action === 'create' || action === 'rename')
  const picking = $derived(action === 'move' || action === 'merge' || step.replace)
  const note = $derived(action !== 'delete' || step.replace ? stepNote(kind, action) : '')
  let text = $state('')
  const options = $derived(editorOptions(choices, text, false, kind === 'category' ? groups : []))

  /** The typed destination counts only while it names one of the choices exactly. */
  function pick(value: string): void {
    text = value
    step.target = matchChoice(choices, value)?.id ?? ''
  }

  /** Enter in the destination field confirms; a held, modified, or claimed Enter does not. */
  function onkeyaction(next: EditorKeyAction, event: KeyboardEvent): void {
    if (next === 'cancel' || !plainEnter(event)) return
    if (next === 'stage') onconfirm()
  }

  /**
   * Focuses the step's first field, selecting a rename's current name so typing replaces it. A
   * step without a field, such as deleting an unused entry, takes focus itself, so Enter confirms.
   */
  function focusField(node: HTMLElement): void {
    const field = node.querySelector('input')
    if (field) field.focus()
    else node.focus()
    if (action === 'rename') field?.select()
  }
</script>

<Card level="inset">
  <section
    class="mf-manager-step"
    aria-labelledby="mf-manager-step"
    tabindex="-1"
    {@attach focusField}
  >
    <h2 id="mf-manager-step">{stepTitle(kind, step)}</h2>
    {#if action === 'delete'}
      <p class="mf-quiet">{transactionCount(step.source?.transaction_count ?? 0)}</p>
    {/if}
    {#if named}
      <TextInput bind:value={step.label} ariaLabel="Name" block />
    {/if}
    {#if action === 'create' && kind === 'category'}
      <SelectDropdown
        value={step.target}
        title="Category group"
        options={choices.map((group) => ({ value: group.id, label: group.label }))}
        onchange={(value) => (step.target = value)}
      />
    {/if}
    {#if picking}
      <EditorCombobox
        label={pickerLabel(action)}
        {options}
        bind:value={() => text, pick}
        valid={ready}
        onaccept={() => undefined}
        {onkeyaction}
      />
    {/if}
    {#if note}<p class="mf-quiet">{note}</p>{/if}
    {#if error}<p class="editing-error" role="alert">{error}</p>{/if}
    <div class="mf-manager-step__actions">
      <Button
        tone={action === 'delete' ? 'danger' : 'info'}
        surface="solid"
        ariaLabel={CONFIRM_LABELS[action]}
        disabled={!ready}
        onclick={onconfirm}
        >{CONFIRM_LABELS[action]}{#snippet trailing()}<KbdBadge
            keys={['Enter']}
          />{/snippet}</Button
      >
      <Button ariaLabel="Back" onclick={onback}
        >Back{#snippet trailing()}<KbdBadge keys={['Esc']} />{/snippet}</Button
      >
    </div>
  </section>
</Card>

<style>
  .mf-manager-step {
    display: grid;
    gap: var(--space-3);
  }
  /* A step without a field holds focus itself; the ring frames the card, clear of its text. */
  .mf-manager-step:focus-visible {
    outline: var(--focus-ring);
    outline-offset: var(--space-3);
  }
  h2 {
    margin: 0;
    font-size: var(--font-size-md);
  }
  .mf-manager-step__actions {
    display: flex;
    gap: var(--space-2);
  }
</style>
