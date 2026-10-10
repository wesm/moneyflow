<script lang="ts">
  import { Button, DetailDrawer, KbdBadge } from '@kenn-io/kit-ui'
  import { onMount, tick, untrack } from 'svelte'

  import ReviewOperation from './ReviewOperation.svelte'
  import WriteStatus from './WriteStatus.svelte'
  import type { EditingController } from '../../lib/controller/editing'
  import type { ProviderWriteController } from '../../lib/controller/provider-write'
  import type { ReviewController } from '../../lib/controller/review'
  import { transactionCount } from '../../lib/editing/toasts'
  import { reviewEnter } from '../../lib/editing/review-keys'

  interface Props {
    /** Review lists the pending edits to commit; write tracks the provider write it starts. */
    mode: 'review' | 'write'
    editing: EditingController
    review: ReviewController
    providerWrite: ProviderWriteController
    providerName: string
    onclose: () => void
    /** Switches the drawer to write mode once a commit starts a provider write. */
    onwrite: () => void
    onreconnect?: (() => void) | undefined
  }

  let { mode, editing, review, providerWrite, providerName, onclose, onwrite, onreconnect }: Props =
    $props()
  const title = $derived(
    mode === 'review' ? 'Review pending changes' : `${providerName} write status`,
  )
  let body = $state<HTMLElement>()
  let expanded = $state('')
  /** True until the first review loads. An Enter before then commits once it has. */
  let initialLoad = true
  let commitRequested = false
  /** True while a commit runs, and for good once one is accepted, so Enter commits once. */
  let committing = false
  let mounted = true
  const surface = () => body?.closest<HTMLElement>('[role="dialog"]') ?? undefined
  const commitButton = () =>
    surface()?.querySelector<HTMLButtonElement>('.mf-review__commit') ?? undefined

  onMount(() => () => {
    mounted = false
    commitRequested = false
  })
  $effect(() => {
    if (mode === 'review') untrack(() => void loadAndFocusCommit())
    else void tick().then(focusSurface)
  })

  /** Keeps focus in the drawer when Commit leaves it for write status. */
  function focusSurface(): void {
    const panel = surface()
    if (panel && !panel.contains(document.activeElement)) panel.focus()
  }

  async function loadAndFocusCommit(): Promise<void> {
    const accepted = await review.load()
    await tick()
    initialLoad = false
    if (!mounted) return
    const button = commitButton()
    if (button && !button.disabled) button.focus()
    if (accepted && commitRequested) {
      commitRequested = false
      await commit()
    }
  }

  async function commit(): Promise<void> {
    const revision = review.state.reviewedRevision
    if (revision === undefined || committing) return
    committing = true
    let accepted = false
    try {
      accepted = await editing.commit(revision)
    } finally {
      committing = accepted
    }
    if (!accepted) return
    review.clear()
    if (!mounted) return
    if (editing.state.providerWrite) onwrite()
    else onclose()
  }

  function keydown(event: KeyboardEvent): void {
    if (mode !== 'review') return
    const decision = reviewEnter(event, surface(), commitButton(), initialLoad)
    if (decision === 'pass') return
    event.preventDefault()
    if (decision === 'swallow') return
    if (initialLoad) commitRequested = true
    else void commit()
  }
</script>

<svelte:window onkeydown={keydown} />

<DetailDrawer {title} ariaLabel={title} {onclose} width="min(960px, 100vw)">
  <div class="mf-review" bind:this={body}>
    {#if mode === 'write'}
      <WriteStatus controller={providerWrite} {providerName} {onreconnect} />
    {:else}
      {#if review.state.phase === 'loading' && review.state.reviewedRevision === undefined}
        <p class="mf-review__quiet">Loading pending changes…</p>
      {/if}
      {#if review.state.announcement}<p role="alert">{review.state.announcement}</p>{/if}
      <section aria-labelledby="mf-review-active">
        <h2 class="mf-review__heading" id="mf-review-active">Active operations</h2>
        <ol class="mf-review__list">
          {#each review.state.activeOperations as operation (operation.operation_id)}
            <ReviewOperation
              {operation}
              {review}
              expanded={expanded === operation.operation_id}
              ontoggle={() =>
                (expanded = expanded === operation.operation_id ? '' : operation.operation_id)}
            />
          {/each}
        </ol>
      </section>
      {#if review.state.inactiveOperations.length}
        <section class="mf-review__redo" aria-labelledby="mf-review-redo">
          <h2 class="mf-review__heading" id="mf-review-redo">Inactive redo operations</h2>
          <p class="mf-review__quiet">Commit permanently discards this redo history.</p>
          <ol class="mf-review__list">
            {#each review.state.inactiveOperations as operation (operation.operation_id)}
              <li>{operation.label} · {transactionCount(operation.affected_count)}</li>
            {/each}
          </ol>
        </section>
      {/if}
    {/if}
  </div>
  {#snippet footer()}
    {#if mode === 'review'}
      <Button onclick={onclose}>Cancel</Button>
      <Button
        class="mf-review__commit"
        tone="success"
        surface="solid"
        ariaLabel="Commit reviewed changes"
        disabled={review.state.reviewedRevision === undefined}
        onclick={() => void commit()}
        >Commit reviewed changes{#snippet trailing()}<KbdBadge keys={['Enter']} />{/snippet}</Button
      >
    {:else}
      <Button onclick={onclose}>Close</Button>
    {/if}
  {/snippet}
</DetailDrawer>

<style>
  .mf-review {
    display: grid;
    gap: var(--space-6);
    padding: var(--space-6) var(--space-7);
    font-size: var(--font-size-md);
  }
  .mf-review__heading {
    margin: 0 0 var(--space-4);
    color: var(--text-secondary);
    font-size: var(--font-size-sm);
    font-weight: var(--font-weight-semibold);
  }
  .mf-review__list {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .mf-review__quiet {
    margin: 0;
    color: var(--text-secondary);
  }
  .mf-review__redo li {
    padding-block: var(--space-2);
    color: var(--text-secondary);
  }
  @media (max-width: 640px) {
    .mf-review {
      padding: var(--space-5) var(--space-6);
    }
  }
</style>
