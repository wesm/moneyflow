<script lang="ts">
  import { Button, Modal, SelectDropdown, TextInput } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'
  import type { EditingController } from '../../lib/controller/editing'

  interface Props {
    controller: EditingController
    target: { kind: string; identity: string }
    hasSelection: boolean
    onclose: () => void
  }
  let { controller, target, hasSelection, onclose }: Props = $props()
  let label = $state('')
  let scope = $state('entity')
  let merchants = $state<Array<{ id: string; label: string }>>([])
  let error = $state('')
  let submitting = $state(false)
  const collision = $derived(
    merchants.find(
      (merchant) => merchant.label.trim().toLocaleLowerCase() === label.trim().toLocaleLowerCase(),
    ),
  )

  onMount(() => {
    if (hasSelection) scope = 'transactions'
    void controller
      .catalog()
      .then((catalog) => (merchants = catalog.merchants ?? []))
      .catch(() => (error = 'Merchant choices could not be loaded.'))
  })

  async function submit(): Promise<void> {
    if (!label.trim()) {
      error = 'Enter a merchant name.'
      return
    }
    submitting = true
    const destination =
      collision?.id ?? (scope === 'transactions' ? `merchant_${crypto.randomUUID()}` : '')
    const accepted = await controller.submit({
      action: 'transaction.edit-merchant',
      target,
      input: {
        scope,
        label: label.trim(),
        ...(destination ? { destination_id: destination } : {}),
      },
    })
    submitting = false
    if (accepted) onclose()
    else error = controller.state.announcement
  }
</script>

<Modal title="Edit merchant" closeLabel="Cancel merchant edit" {onclose}>
  <form
    class="editing-form"
    onsubmit={(event) => {
      event.preventDefault()
      void submit()
    }}
  >
    <label for="merchant-name">Merchant name</label>
    <TextInput id="merchant-name" bind:value={label} block autofocus invalid={!!error} />
    <span class="editing-label">Scope</span>
    <SelectDropdown
      value={scope}
      title="Merchant edit scope"
      options={[
        { value: 'entity', label: 'Whole merchant' },
        {
          value: 'transactions',
          label: hasSelection ? 'Selected transactions' : 'Focused transaction',
        },
      ]}
      onchange={(value) => (scope = value)}
    />
    {#if collision}<p role="status">This will merge or reassign into {collision.label}.</p>{/if}
    {#if error}<p class="editing-error" role="alert">{error}</p>{/if}
    <div class="editing-actions">
      <Button type="button" onclick={onclose}>Cancel</Button><Button
        type="submit"
        tone="info"
        surface="solid"
        disabled={submitting}>Save pending change</Button
      >
    </div>
  </form>
</Modal>
