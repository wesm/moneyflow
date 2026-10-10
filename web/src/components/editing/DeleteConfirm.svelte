<script lang="ts">
  import { Button, KbdBadge } from '@kenn-io/kit-ui'

  import AnchoredEditor from './AnchoredEditor.svelte'
  import BottomSheet from '../shell/BottomSheet.svelte'
  import { transactionCount } from '../../lib/editing/toasts'
  import type { EditorAnchor, OverlayState } from '../../lib/shell/overlay-state.svelte'

  interface Props {
    /** How many transactions the delete stages. */
    count: number
    /** At phone width the confirmation is a bottom sheet. */
    compact: boolean
    /** On desktop, the cursor row it hangs below or the selection bar it sits above. */
    anchor: EditorAnchor | undefined
    overlays: OverlayState
    /** Stages the delete. Resolves whether the server accepted it. */
    onconfirm: () => Promise<boolean>
    /** Esc, Cancel, or a press on the phone sheet's scrim. */
    oncancel: () => void
    /** A press outside the desktop popover, which leaves focus where it lands. */
    ondismiss: () => void
  }

  let { count, compact, anchor, overlays, onconfirm, oncancel, ondismiss }: Props = $props()
  const id = 'mf-delete'
  let body = $state<HTMLElement>()
  /** True while staging runs, and for good once it is accepted, so the delete stages once. */
  let staging = false

  async function confirm(): Promise<void> {
    if (staging) return
    staging = true
    let accepted = false
    try {
      accepted = await onconfirm()
    } finally {
      staging = accepted
    }
  }

  /**
   * Enter stages from anywhere in the confirmation except Cancel, which keeps its own Enter.
   * The browser's activation of a focused Delete is prevented, so one Enter stages once. A held
   * Enter's repeats stage nothing.
   */
  function keydown(event: KeyboardEvent): void {
    if (event.key !== 'Enter' || event.defaultPrevented) return
    const target = event.target
    const panel = body?.closest('[role="alertdialog"]')
    if (!(target instanceof Node) || !(panel?.contains(target) || target === document.body)) return
    if (target instanceof HTMLButtonElement && !target.classList.contains('mf-delete__confirm'))
      return
    event.preventDefault()
    if (event.repeat || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return
    void confirm()
  }
</script>

<svelte:window onkeydown={keydown} />

{#snippet confirmation()}
  <div class="mf-delete" bind:this={body}>
    <h2 class="kit-sr-only" id={`${id}-title`}>Confirm deletion</h2>
    <p class="mf-delete__question" id={`${id}-count`}>Delete {transactionCount(count)}?</p>
    <div class="mf-delete__actions">
      <Button
        class="mf-delete__confirm"
        tone="danger"
        surface="solid"
        ariaLabel="Delete"
        label="Delete"
        onclick={() => void confirm()}
      >
        {#snippet trailing()}<KbdBadge keys={['Enter']} />{/snippet}
      </Button>
      <Button ariaLabel="Cancel" label="Cancel" onclick={oncancel}>
        {#snippet trailing()}<KbdBadge keys={['Esc']} />{/snippet}
      </Button>
    </div>
  </div>
{/snippet}

{#if compact}
  <BottomSheet
    open
    role="alertdialog"
    labelledby={`${id}-title`}
    describedby={`${id}-count`}
    {overlays}
    onclose={oncancel}
  >
    {@render confirmation()}
  </BottomSheet>
{:else}
  <AnchoredEditor
    anchor={anchor?.element}
    above={anchor?.above ?? false}
    field="merchant"
    {id}
    role="alertdialog"
    {overlays}
    {oncancel}
    {ondismiss}
  >
    {@render confirmation()}
  </AnchoredEditor>
{/if}

<style>
  .mf-delete {
    display: grid;
    gap: var(--space-3);
  }
  .mf-delete__question {
    margin: 0;
    font-weight: var(--font-weight-semibold);
  }
  .mf-delete__actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
</style>
