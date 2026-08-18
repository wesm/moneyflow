<script lang="ts" module>
  function inputTarget(target: EventTarget | null): boolean {
    return target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement
  }
</script>

<script lang="ts">
  import { Button, Card } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'

  interface Props {
    onselect: (provider: 'monarch' | 'local') => void
    onback: () => void
  }

  let { onselect, onback }: Props = $props()
  let announcement = $state('Monarch is available. YNAB and SimpleFIN are planned.')
  let active = $state(0)
  let list = $state<HTMLElement | undefined>()
  const providers = [
    { key: 'monarch', name: 'Monarch Money', shortcut: 'm', available: true },
    { key: 'ynab', name: 'YNAB', shortcut: 'y', available: false },
    { key: 'simplefin', name: 'SimpleFIN', shortcut: 's', available: false },
    { key: 'local', name: 'Local only', shortcut: 'l', available: true },
  ] as const

  function choose(index: number): void {
    active = index
    const provider = providers[index]!
    if (provider.available && (provider.key === 'monarch' || provider.key === 'local')) {
      onselect(provider.key)
      return
    }
    announcement = `${provider.name} is not available in Go yet.`
    focusActive()
  }

  function keydown(event: KeyboardEvent): void {
    if (event.defaultPrevented || inputTarget(event.target)) return
    const shortcut = providers.findIndex(
      (provider) => provider.shortcut === event.key.toLowerCase(),
    )
    if (shortcut >= 0) {
      event.preventDefault()
      choose(shortcut)
      return
    }
    if (event.key === 'ArrowDown' || event.key === 'j') active = (active + 1) % providers.length
    else if (event.key === 'ArrowUp' || event.key === 'k')
      active = (active - 1 + providers.length) % providers.length
    else if (event.key === 'Home') active = 0
    else if (event.key === 'Enter') return choose(active)
    else if (event.key === 'Escape') return onback()
    else return
    event.preventDefault()
    focusActive()
  }

  function focusActive(): void {
    list?.querySelectorAll<HTMLButtonElement>('button').item(active).focus()
  }

  onMount(focusActive)
</script>

<svelte:window onkeydown={keydown} />

<section class="profile-panel" aria-labelledby="provider-title">
  <p class="moneyflow-eyebrow">Add profile</p>
  <h1 id="provider-title">Choose a provider</h1>
  <p>Provider adapters stay explicit. Unavailable choices remain keyboard focusable.</p>
  <div class="profile-list" role="list" bind:this={list}>
    {#each providers as provider, index (provider.key)}
      <Card
        title={provider.name}
        meta={provider.available ? 'Available' : 'Coming later'}
        selected={active === index}
        ariaLabel={`${provider.name}, ${provider.available ? 'available' : 'not available'}`}
        onclick={() => choose(index)}
      >
        <span>{provider.shortcut} · {provider.available ? 'Continue' : 'Learn status'}</span>
      </Card>
    {/each}
  </div>
  <div class="profile-actions"><Button onclick={onback}>Back</Button></div>
  <p class="kit-sr-only" role="status" aria-live="polite">{announcement}</p>
</section>
