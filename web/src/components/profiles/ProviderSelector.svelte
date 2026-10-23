<script lang="ts" module>
  function inputTarget(target: EventTarget | null): boolean {
    return target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement
  }
</script>

<script lang="ts">
  import { Button, Card } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'

  interface Props {
    onselect: (provider: 'monarch' | 'ynab' | 'simplefin' | 'amazon' | 'local') => void
    onback: () => void
  }

  let { onselect, onback }: Props = $props()
  const announcement = 'Choose a provider. SimpleFIN is experimental and read-only.'
  let active = $state(0)
  let list = $state<HTMLElement | undefined>()
  // What each provider's setup asks for, so the user can have it ready.
  const providers = [
    {
      key: 'monarch',
      name: 'Monarch Money',
      shortcut: 'm',
      needs: 'Monarch email, password and authenticator setup key',
    },
    {
      key: 'amazon',
      name: 'Amazon orders',
      shortcut: 'a',
      needs: 'Order history files from Amazon',
    },
    { key: 'ynab', name: 'YNAB', shortcut: 'y', needs: 'YNAB personal access token' },
    {
      key: 'simplefin',
      name: 'SimpleFIN (experimental)',
      shortcut: 's',
      needs: 'SimpleFIN setup token. Read-only.',
    },
    { key: 'local', name: 'Local only', shortcut: 'l', needs: 'No provider. Edits stay local.' },
  ] as const

  function choose(index: number): void {
    active = index
    const provider = providers[index]!
    onselect(provider.key)
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
  <p>Connect an account, import orders, or work locally.</p>
  <div class="profile-list" role="list" bind:this={list}>
    {#each providers as provider, index (provider.key)}
      <Card
        title={provider.name}
        meta={provider.shortcut}
        selected={active === index}
        onclick={() => choose(index)}
      >
        <span>{provider.needs}</span>
      </Card>
    {/each}
  </div>
  <div class="profile-actions"><Button onclick={onback}>Back</Button></div>
  <p class="kit-sr-only" role="status" aria-live="polite">{announcement}</p>
</section>
