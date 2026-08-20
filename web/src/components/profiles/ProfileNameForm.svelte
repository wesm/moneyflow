<script lang="ts">
  import { Button, TextInput } from '@kenn-io/kit-ui'

  interface Props {
    provider: 'monarch' | 'amazon' | 'local'
    onsubmit: (name: string) => void
    onback: () => void
  }

  let { provider, onsubmit, onback }: Props = $props()
  let name = $state('')
  let error = $state('')

  function submit(): void {
    const value = name.trim()
    if (!value) {
      error = 'Enter a profile name.'
      return
    }
    onsubmit(value)
  }
</script>

<section class="profile-panel" aria-labelledby="profile-name-title">
  <p class="moneyflow-eyebrow">
    {provider === 'monarch'
      ? 'Monarch Money'
      : provider === 'amazon'
        ? 'Amazon orders'
        : 'Local profile'}
  </p>
  <h1 id="profile-name-title">Name this profile</h1>
  <form
    class="profile-form"
    onsubmit={(event) => {
      event.preventDefault()
      submit()
    }}
  >
    <label for="new-profile-name">Profile name</label>
    <TextInput id="new-profile-name" bind:value={name} block autofocus autocomplete="off" />
    {#if error}<p role="alert" class="editing-error">{error}</p>{/if}
    <div class="profile-actions">
      <Button type="button" onclick={onback}>Back</Button>
      <Button type="submit" tone="info" surface="solid">Create profile</Button>
    </div>
  </form>
</section>
