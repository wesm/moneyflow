<script lang="ts">
  import { Button, SegmentedControl, SelectDropdown } from '@kenn-io/kit-ui'
  import { onMount, tick, untrack } from 'svelte'

  import EditorCombobox from './EditorCombobox.svelte'
  import EditorPreview from './EditorPreview.svelte'
  import type { EditorCatalog, ViewProjection } from '../../lib/api/client'
  import type { EditingController } from '../../lib/controller/editing'
  import {
    countLine,
    editorIntent,
    impactLines,
    matchChoice,
    scopeOptions,
    stageLabel,
    type EditorKind,
    type EditScope,
  } from '../../lib/editing/editor-copy'
  import { controlKey, type EditorKeyAction } from '../../lib/editing/editor-keys'
  import { merchantScope } from '../../lib/editing/merchant-scope'
  import { editorOptions, type EditorOption } from '../../lib/editing/options'
  import { ScopePreviews } from '../../lib/editing/scope-previews.svelte'
  import { StageQueue } from '../../lib/editing/stage-queue.svelte'

  interface Props {
    kind: EditorKind
    target: { kind: 'transaction' | 'aggregate'; identity: string }
    controller: EditingController
    /** The view the editor opened over. */
    projection: ViewProjection
    cursorIndex: number
    /** Prefix of the title and count IDs, which name and describe the host dialog. */
    id: string
    onstage: () => void
    oncancel: () => void
  }

  let { kind, target, controller, projection, cursorIndex, id, onstage, oncancel }: Props = $props()

  // The editor edits what it opened over; a later projection does not retarget it.
  const opened = untrack(() => ({ kind, target, view: projection, cursor: cursorIndex }))
  const selecting = opened.view.selection_count > 0
  const rule = merchantScope(opened.view, opened.cursor, opened.view.selection_count)
  const count = countLine(opened.view, opened.target.identity)
  const currency = opened.view.view.currency?.currency
  let catalog = $state<EditorCatalog>()
  let text = $state('')
  let scope = $state<EditScope>('transactions')
  const previews = new ScopePreviews((scope, signal) =>
    controller.previewMerchant(opened.target, scope, signal),
  )
  let error = $state('')
  let submitting = $state(false)
  /** The name a `Create "…"` accepted; the group step shows while the text still reads it. */
  let created = $state<string>()
  let groupId = $state('')
  let field = $state<HTMLElement>()
  let groupStep = $state<HTMLElement>()

  const merchant = opened.kind === 'merchant'
  const choices = $derived((merchant ? catalog?.merchants : catalog?.categories) ?? [])
  const groups = $derived(catalog?.groups ?? [])
  // Until the choices load, a typed name could duplicate one that exists, so nothing is offered.
  const options = $derived(editorOptions(choices, text, !!catalog, merchant ? [] : groups))
  const match = $derived(matchChoice(choices, text))
  const creating = $derived(!merchant && !match && created !== undefined && created === text.trim())
  const preview = $derived(previews.byScope[scope])
  const named = $derived(merchant ? text.trim() !== '' : match !== undefined || creating)
  /** Whether Enter may ask to stage; a request made while loading waits for `valid`. */
  const stageable = $derived(!submitting && named && (!creating || !!groupId))
  const valid = $derived(stageable && !!catalog && !!preview)
  const queue = new StageQueue()
  const impact = $derived(merchant ? impactLines(preview, scope, match?.label, currency) : [])
  const counts = $derived({
    transactions: previews.byScope.transactions?.affected_transactions,
    entity: previews.byScope.entity?.affected_transactions,
  })

  onMount(() => {
    field?.querySelector('input')?.focus()
    controller
      .catalog()
      .then((value) => (catalog = value))
      .catch(() => (error = `${merchant ? 'Merchant' : 'Category'} choices could not be loaded.`))
    if (!merchant || !rule.entity) return
    // The whole merchant's count labels its scope before the user picks it.
    return previews.load('entity')
  })

  // Every scope change re-requests its preview, aborting the earlier request.
  $effect(() => {
    const current = scope
    return untrack(() => previews.load(current))
  })

  $effect(() => {
    if (queue.settle({ text, scope }, stageable, valid)) void stage()
  })

  async function stage(): Promise<void> {
    if (!stageable) return
    if (!catalog || !preview) return queue.request({ text, scope })
    if (BigInt(preview.revision) !== controller.state.revision) {
      error = 'The profile changed. Reopen this editor to review affected transactions.'
      return
    }
    submitting = true
    error = ''
    const intent = editorIntent({
      kind: opened.kind,
      target: opened.target,
      text,
      scope: merchant ? scope : '',
      match,
      groupId,
    })
    const failed = 'The change could not be staged.'
    try {
      if (!(await controller.submit(intent))) error = controller.state.announcement || failed
    } catch {
      // A request that never answered left no announcement of its own to show.
      error = failed
    } finally {
      submitting = false
    }
    if (!error) onstage()
  }

  /** Accepting Create for a category opens the group step; a merchant name needs no group. */
  function onaccept(option: EditorOption): void {
    if (merchant || !option.create) return
    created = option.label
    if (!groupId) groupId = groups[0]?.id ?? ''
    void tick().then(() => groupStep?.querySelector('button')?.focus())
  }

  function onkeyaction(action: EditorKeyAction, event: KeyboardEvent): void {
    if (action === 'cancel') {
      // Claimed here, so the overlay around the editor does not cancel it a second time.
      event.preventDefault()
      event.stopPropagation()
      oncancel()
      return
    }
    // A new category still needs its group, so accepting Create leads to the group step.
    if (action === 'accept-and-stage' && creating) return
    void stage()
  }

  /** Esc on the group step returns to the field. */
  function back(): void {
    created = undefined
    void tick().then(() => field?.querySelector('input')?.focus())
  }

  /** Keys outside the field, which handles its own: Enter on the scope, Ctrl+Enter, and Esc. */
  function keys(node: HTMLElement): () => void {
    const keydown = (event: KeyboardEvent) => {
      const control = event.target
      if (!(control instanceof HTMLElement) || field?.contains(control)) return
      const action = controlKey(event, control, stageable)
      if (action === 'none') return
      event.preventDefault()
      event.stopPropagation()
      if (action === 'stage') void stage()
      else if (groupStep?.contains(control)) back()
      else oncancel()
    }
    node.addEventListener('keydown', keydown)
    return () => node.removeEventListener('keydown', keydown)
  }
</script>

<div class="mf-editor" {@attach keys}>
  <div class="mf-editor__head">
    <h2 class="mf-editor__title" id={`${id}-title`}>
      {merchant ? 'Edit merchant' : 'Change category'}
    </h2>
    <span class="mf-editor__quiet" id={`${id}-count`}>{count}</span>
  </div>
  <div bind:this={field}>
    <EditorCombobox
      label={merchant ? 'Merchant' : 'Category'}
      {options}
      bind:value={text}
      valid={stageable}
      {onaccept}
      {onkeyaction}
    />
  </div>
  {#if creating}
    <div class="mf-editor__group" bind:this={groupStep}>
      <span class="mf-editor__quiet">Group</span>
      <SelectDropdown
        value={groupId}
        title="Category group"
        options={groups.map((group) => ({ value: group.id, label: group.label }))}
        onchange={(value) => (groupId = value)}
      />
    </div>
  {/if}
  {#if merchant}
    <SegmentedControl
      block
      ariaLabel="Scope"
      options={scopeOptions(rule, counts, selecting)}
      value={scope}
      onchange={(value) => (scope = value as EditScope)}
    />
    {#if rule.reason}<p class="mf-editor__quiet">{rule.reason}</p>{/if}
  {/if}
  <EditorPreview {impact} {preview} error={previews.error} />
  {#if error}<p class="editing-error" role="alert">{error}</p>{/if}
  <div class="mf-editor__actions">
    <Button tone="info" surface="solid" disabled={!valid} onclick={() => void stage()}>
      {stageLabel(preview?.affected_transactions)}
    </Button>
    <Button onclick={oncancel}>Cancel</Button>
  </div>
</div>

<style>
  .mf-editor {
    display: grid;
    gap: var(--space-3);
  }
  .mf-editor p {
    margin: 0;
  }
  .mf-editor__head,
  .mf-editor__group {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
  }
  .mf-editor__group {
    align-items: center;
  }
  .mf-editor__title {
    margin: 0;
    font-size: var(--font-size-md);
  }
  .mf-editor__quiet {
    color: var(--text-secondary);
    font-size: var(--font-size-xs);
  }
  .mf-editor__actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
</style>
