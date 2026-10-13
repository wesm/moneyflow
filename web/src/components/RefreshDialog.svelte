<script lang="ts">
  import { Button, KbdBadge, Modal } from '@kenn-io/kit-ui'
  import { tick } from 'svelte'

  import RefreshProgress from './RefreshProgress.svelte'
  import type { ProviderController, RefreshDialogView } from '../lib/controller/provider'
  import { dataAge, providerLabel } from '../lib/controller/refresh-notice'

  interface Props {
    controller: ProviderController
    /** Opens the provider's reconnect flow. */
    onreconnect?: (() => void) | undefined
  }

  let { controller, onreconnect }: Props = $props()
  const providerState = $derived(controller.state)
  const dialog = $derived(providerState.dialog)
  const view = $derived(dialog?.view)
  const status = $derived(providerState.status)
  const provider = $derived(providerLabel(status?.provider_kind))
  let now = $state(Date.now())
  let body = $state<HTMLElement>()
  /** What screen readers last heard about progress; it changes at most every ten seconds. */
  let spoken = $state(progressText())
  const age = $derived(status?.last_success ? dataAge(status.last_success, now) : undefined)
  const running = $derived(view === 'progress' || view === 'stopping' || view === 'other')
  const title = $derived(titleFor(view))
  /** Status checks fail, so a running refresh's end cannot be seen; the user may leave. */
  const unchecked = $derived(
    (view === 'stopping' || view === 'other') && providerState.statusError !== undefined,
  )
  const continueLabel = $derived(age ? `Continue with data from ${age.date}` : 'Continue')

  function titleFor(current: RefreshDialogView | undefined): string {
    if (current === 'failed') return `Refresh from ${provider} failed`
    if (current === 'reconnect') return `Reconnect ${provider}`
    if (current === 'confirmation') return 'Confirm provider refresh'
    return `Refreshing from ${provider}`
  }

  function progressText(): string {
    const progress = controller.state.status?.progress
    if (!progress || progress.total === 0) return ''
    return `Fetched ${progress.fetched} of ${progress.total}`
  }

  $effect(() => {
    if (!running) return
    const clock = setInterval(() => (now = Date.now()), 1_000)
    const speech = setInterval(() => (spoken = progressText()), 10_000)
    return () => {
      clearInterval(clock)
      clearInterval(speech)
    }
  })

  // A new view replaces the buttons focus was on; its first action, or the dialog, takes focus.
  $effect(() => {
    if (!view) return
    void tick().then(() => {
      const panel = body?.closest<HTMLElement>('[role="dialog"]')
      const active = document.activeElement
      if (!panel || (active !== panel && panel.contains(active))) return
      ;(panel.querySelector<HTMLElement>('.mf-refresh__focus') ?? panel).focus()
    })
  })

  function close(): void {
    if (view === 'progress') controller.cancel()
    else if (view === 'confirmation') controller.dismissConfirmation()
    else controller.continueStale()
  }

  function reconnect(): void {
    controller.continueStale()
    onreconnect?.()
  }

  /**
   * Escape belongs to this dialog, the top layer, even when an overlay under it, such as duplicate
   * review, registered its own window handler first. Taking it during capture keeps it from them.
   */
  function escape(event: KeyboardEvent): void {
    if (!dialog || event.key !== 'Escape') return
    event.preventDefault()
    event.stopImmediatePropagation()
    close()
  }

  /** Enter confirms removals from anywhere in the dialog except another button. */
  function keydown(event: KeyboardEvent): void {
    if (view !== 'confirmation' || event.key !== 'Enter' || event.defaultPrevented) return
    const target = event.target
    if (target instanceof HTMLButtonElement && !target.classList.contains('mf-refresh__confirm'))
      return
    event.preventDefault()
    event.stopPropagation()
    if (!event.repeat) void controller.confirm()
  }
</script>

<svelte:window onkeydown={keydown} onkeydowncapture={escape} />

{#if dialog}
  <Modal
    {title}
    tone={view === 'failed' ? 'danger' : view === 'reconnect' ? 'warning' : 'neutral'}
    closable={false}
    closeOnOverlayClick={false}
    onclose={close}
  >
    <div class="mf-refresh" bind:this={body}>
      {#if running}
        <RefreshProgress {controller} {now} />
        {#if unchecked}
          <p class="mf-refresh__lead" role="alert">{providerState.statusError}</p>
        {/if}
      {:else if view === 'confirmation'}
        <p class="mf-refresh__lead">
          This refresh would remove {status?.summary.removed_transactions ?? 0} posted transactions.
        </p>
        <p>Review the count before accepting this provider snapshot.</p>
      {:else}
        <p class="mf-refresh__lead" role="alert">{dialog.reason}</p>
        {#if view === 'reconnect'}
          <p>Reconnect to sign in again. Your data and pending edits stay as they are.</p>
        {/if}
      {/if}
      {#if view !== 'confirmation'}
        <p class="mf-refresh__meta">
          {age ? `Last updated ${age.relative} (${age.absolute})` : 'No earlier refresh'}
        </p>
      {/if}
      <p class="kit-sr-only" aria-live="polite" data-testid="refresh-live">{spoken}</p>
    </div>
    {#snippet footer()}
      {#if view === 'progress'}
        <Button ariaLabel="Cancel refresh" label="Cancel" onclick={() => controller.cancel()}>
          {#snippet trailing()}<KbdBadge keys={['Esc']} />{/snippet}
        </Button>
      {:else if unchecked}
        <Button class="mf-refresh__focus" onclick={() => controller.continueStale()}
          >{continueLabel}</Button
        >
      {:else if view === 'stopping'}
        <Button disabled label="Stopping…" />
      {:else if view === 'failed' || view === 'reconnect'}
        <Button onclick={() => controller.continueStale()}>{continueLabel}</Button>
        {#if view === 'reconnect' && onreconnect}
          <Button class="mf-refresh__focus" tone="info" surface="solid" onclick={reconnect}
            >Reconnect</Button
          >
        {:else}
          <Button
            class="mf-refresh__focus"
            tone="info"
            surface="solid"
            onclick={() => void controller.refresh()}>Retry</Button
          >
        {/if}
      {:else if view === 'confirmation'}
        <Button ariaLabel="Cancel" label="Cancel" onclick={() => controller.dismissConfirmation()}>
          {#snippet trailing()}<KbdBadge keys={['Esc']} />{/snippet}
        </Button>
        <Button
          class="mf-refresh__confirm"
          tone="danger"
          surface="solid"
          ariaLabel="Confirm refresh"
          label="Confirm refresh"
          onclick={() => void controller.confirm()}
        >
          {#snippet trailing()}<KbdBadge keys={['Enter']} />{/snippet}
        </Button>
      {/if}
    {/snippet}
  </Modal>
{/if}
