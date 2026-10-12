<script lang="ts">
  import { Button, DetailDrawer, KbdBadge, SearchInput } from '@kenn-io/kit-ui'
  import { onMount, tick } from 'svelte'

  import ManagerList from './ManagerList.svelte'
  import ManagerStep from './ManagerStep.svelte'
  import type { EditorCatalog } from '../../lib/api/client'
  import { REQUEST_FAILED, type EditingController } from '../../lib/controller/editing'
  import {
    ACTION_LABELS,
    canStart,
    deleteDetail,
    managerActions,
    managerChoices,
    managerIntent,
    managerRows,
    managerShortcut,
    stepChoices,
    stepEnter,
    stepProblem,
    stepReady,
    startStep,
    type ManagerAction,
    type ManagerKind,
    type ManagerStep as Step,
  } from '../../lib/editing/manager'
  import type { OverlayState } from '../../lib/shell/overlay-state.svelte'

  interface Props {
    kind: ManagerKind
    controller: EditingController
    overlays: OverlayState
    onclose: () => void
  }

  let { kind, controller, overlays, onclose }: Props = $props()
  const category = $derived(kind === 'category')
  const title = $derived(category ? 'Manage categories' : 'Manage category groups')
  let catalog = $state<EditorCatalog>()
  let filter = $state('')
  let activeID = $state('')
  let step = $state<Step>()
  let error = $state('')
  let staging = $state(false)
  let filterInput = $state<HTMLInputElement>()
  let list = $state<HTMLElement>()

  const groups = $derived(catalog?.groups ?? [])
  const choices = $derived(managerChoices(catalog, kind))
  const rows = $derived(managerRows(choices, filter, groups))
  const activeIndex = $derived(
    Math.max(
      0,
      rows.findIndex((row) => row.id === activeID),
    ),
  )
  const source = $derived(choices.find((choice) => choice.id === rows[activeIndex]?.id))
  const ready = $derived(!staging && step !== undefined && stepReady(step, kind, choices))
  const problem = $derived(step ? stepProblem(step, choices) : '')
  const loadFailed = $derived(`${category ? 'Category' : 'Group'} choices could not be loaded.`)

  let loads = 0

  onMount(() => void load())

  /** Returns false only when the newest load fails; an older answer that arrives later is dropped. */
  async function load(): Promise<boolean> {
    const request = ++loads
    try {
      const next = await controller.catalog()
      if (request === loads) catalog = next
      return true
    } catch {
      if (request !== loads) return true
      error = loadFailed
      return false
    }
  }

  function start(action: ManagerAction): void {
    if (!catalog || !canStart(action, source)) return
    error = ''
    if (action !== 'create') activeID = source?.id ?? ''
    step = startStep(kind, action, action === 'create' ? undefined : source, catalog)
  }

  /** Closes the step and returns to the list. */
  function back(): void {
    step = undefined
    error = ''
    void tick().then(() => list?.focus())
  }

  async function confirm(): Promise<void> {
    if (!step || !ready) return
    const newID = `${kind}_${crypto.randomUUID()}`
    const current = step
    const { action, target } = step
    staging = true
    error = ''
    let accepted = false
    try {
      accepted = await controller.submit(managerIntent(kind, step, newID))
    } catch {
      // A request that never answered left no announcement of its own.
    } finally {
      staging = false
    }
    // Esc may have left this step while it staged; its answer then belongs to no open step.
    if (!accepted) {
      if (step === current) error = controller.state.announcement || REQUEST_FAILED
      return
    }
    if (action === 'create') activeID = newID
    else if (action === 'merge' || action === 'delete') activeID = target
    const loaded = await load()
    // Esc and a new step's key still work during the reload; leave that new step open.
    if (step === current) back()
    if (!loaded) error = loadFailed
  }

  /** Esc steps out of an open step, then clears the filter, then closes the drawer. */
  function escapeStep(): void {
    if (step) back()
    else if (filter) filter = ''
    else onclose()
  }

  /**
   * The manager's keys, on its whole panel. Escape steps back as `escapeStep` says. With a
   * step open, Enter confirms it; otherwise n, r, g, m, and d start a step. Claiming these keys
   * keeps them from the table's own g, m, and d.
   */
  function keys(node: HTMLElement): () => void {
    const panel = node.closest<HTMLElement>('[role="dialog"]') ?? node
    const escape = overlays.escapeLayer(escapeStep)(panel)
    const keydown = (event: KeyboardEvent) => {
      if (step) {
        const decision = stepEnter(event, panel, staging)
        if (decision === 'pass') return
        event.preventDefault()
        if (decision === 'confirm') void confirm()
        return
      }
      const action = event.defaultPrevented ? undefined : managerShortcut(event, kind)
      if (!action) return
      event.preventDefault()
      start(action)
    }
    panel.addEventListener('keydown', keydown)
    return () => {
      panel.removeEventListener('keydown', keydown)
      escape?.()
    }
  }
</script>

<DetailDrawer {title} ariaLabel={title} {onclose} width="min(960px, 100vw)">
  <div class="mf-manager" {@attach keys}>
    <!-- prettier-ignore -->
    <SearchInput
      bind:value={filter}
      bind:inputEl={() => filterInput as HTMLInputElement, (element) => (filterInput = element)}
      ariaLabel={category ? 'Filter categories' : 'Filter groups'}
      placeholder="Filter"
      block
      autofocus
      onkeydown={(event) => {
        if (event.key !== 'ArrowDown') return
        event.preventDefault()
        list?.focus()
      }}
    />
    {#if step}
      <ManagerStep
        {kind}
        bind:step
        choices={stepChoices(step, kind, catalog)}
        {groups}
        {ready}
        detail={step.source ? deleteDetail(kind, step.source, catalog) : ''}
        error={error || problem}
        onconfirm={() => void confirm()}
        onback={back}
      />
    {:else}
      <div class="mf-manager__tools">
        {#each managerActions(kind) as { action, key } (action)}
          <Button
            size="sm"
            ariaLabel={ACTION_LABELS[action]}
            disabled={!catalog || !canStart(action, source)}
            onclick={() => start(action)}
            >{ACTION_LABELS[action]}{#snippet trailing()}<KbdBadge keys={[key]} />{/snippet}</Button
          >
        {/each}
      </div>
      {#if error}<p class="editing-error" role="alert">{error}</p>{/if}
    {/if}
    {#if catalog && !rows.length}<p class="mf-quiet">No matches.</p>{/if}
    <ManagerList
      label={category ? 'Categories' : 'Category groups'}
      {rows}
      {activeIndex}
      bind:activeID
      bind:element={list}
      onleave={() => filterInput?.focus()}
    />
  </div>
</DetailDrawer>

<style>
  /* Fills the drawer body, where only the list scrolls. */
  .mf-manager {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--space-4);
    min-height: 0;
    padding: var(--space-6);
    font-size: var(--font-size-md);
  }
  .mf-manager__tools {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
</style>
