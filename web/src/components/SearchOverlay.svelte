<script lang="ts">
  import { Button, Modal, SearchInput } from '@kenn-io/kit-ui'
  import { onMount, untrack } from 'svelte'

  import { createSearchCoordinator } from '../lib/controller/search'
  import type { ViewController } from '../lib/controller/view-controller.svelte'

  interface Props {
    controller: ViewController
    onclose: () => void
  }

  let { controller, onclose }: Props = $props()
  const initialController = untrack(() => controller)
  const search = createSearchCoordinator(
    initialController,
    initialController.projection?.view.search ?? '',
  )
  let value = $state(search.value)
  let error = $state(search.error)
  let pending = $state(search.pending)

  function sync(): void {
    value = search.value
    error = search.error
    pending = search.pending
  }

  function cancel(): void {
    search.cancel()
    onclose()
  }

  async function commit(): Promise<void> {
    if (await search.commit()) onclose()
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key !== 'Enter') return
    event.preventDefault()
    void commit()
  }

  onMount(() => {
    const unsubscribe = search.subscribe(sync)
    const captureEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      event.stopImmediatePropagation()
      cancel()
    }
    window.addEventListener('keydown', captureEscape, { capture: true })
    return () => {
      window.removeEventListener('keydown', captureEscape, { capture: true })
      unsubscribe()
      search.destroy()
    }
  })
</script>

<Modal title="Search transactions" closeLabel="Cancel search" onclose={cancel}>
  <div class="overlay-form">
    <SearchInput
      {value}
      autofocus
      block
      invalid={error !== ''}
      ariaLabel="Search transactions"
      placeholder="Regular expression…"
      oninput={(next) => {
        search.input(next)
        sync()
      }}
      onkeydown={keydown}
    />
    <p class="overlay-status" class:overlay-status--error={error !== ''} aria-live="polite">
      {error || (pending ? 'Updating preview…' : 'Enter applies · Esc cancels')}
    </p>
  </div>
  {#snippet footer()}
    <Button onclick={cancel}>Cancel</Button>
    <Button tone="info" surface="solid" onclick={() => void commit()}>Apply search</Button>
  {/snippet}
</Modal>
