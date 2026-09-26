<script lang="ts">
  import { Button, Modal, SearchInput, SelectDropdown, TextInput } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'
  import type { EditorCatalog } from '../../lib/api/client'
  import type { EditingController } from '../../lib/controller/editing'

  interface Props {
    controller: EditingController
    target: { kind: string; identity: string }
    onclose: () => void
  }
  let { controller, target, onclose }: Props = $props()
  let catalog = $state<EditorCatalog | undefined>()
  let destination = $state('')
  let label = $state('')
  let query = $state('')
  let groupID = $state('')
  let error = $state('')
  const categories = $derived(
    (catalog?.categories ?? []).filter((choice) =>
      choice.label.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
    ),
  )
  $effect(() => {
    if (destination === '__new__') return
    if (!categories.some((choice) => choice.id === destination)) {
      destination = categories[0]?.id ?? '__new__'
    }
  })
  onMount(() => {
    void controller
      .catalog()
      .then((value) => {
        catalog = value
        destination = value.categories?.[0]?.id ?? ''
        groupID = value.groups?.[0]?.id ?? ''
      })
      .catch(() => (error = 'Category choices could not be loaded.'))
  })
  async function submit(): Promise<void> {
    const creating = destination === '__new__'
    if (creating && (!label.trim() || !groupID)) {
      error = 'Enter a category name and choose a group.'
      return
    }
    const accepted = await controller.submit({
      action: 'transaction.edit-category',
      target,
      input: {
        scope: 'transactions',
        destination_id: creating ? `category_${crypto.randomUUID()}` : destination,
        ...(creating ? { label: label.trim(), group_id: groupID } : {}),
      },
    })
    if (accepted) onclose()
    else error = controller.state.announcement
  }
</script>

<Modal title="Change category" closeLabel="Cancel category change" {onclose}>
  <form
    class="editing-form"
    onsubmit={(event) => {
      event.preventDefault()
      void submit()
    }}
  >
    <span class="editing-label">Category</span>
    <SearchInput
      value={query}
      block
      ariaLabel="Filter categories"
      oninput={(value) => (query = value)}
    />
    <SelectDropdown
      value={destination}
      title="Category"
      options={[
        ...categories.map((choice) => ({ value: choice.id, label: choice.label })),
        { value: '__new__', label: 'Create new category…' },
      ]}
      onchange={(value) => (destination = value)}
    />
    {#if destination === '__new__'}
      <label for="new-category-name">New category name</label><TextInput
        id="new-category-name"
        bind:value={label}
        block
        autofocus
      />
      <span class="editing-label">Category group</span><SelectDropdown
        value={groupID}
        title="Category group"
        options={(catalog?.groups ?? []).map((choice) => ({
          value: choice.id,
          label: choice.label,
        }))}
        onchange={(value) => (groupID = value)}
      />
    {/if}
    {#if error}<p class="editing-error" role="alert">{error}</p>{/if}
    <div class="editing-actions">
      <Button type="button" onclick={onclose}>Cancel</Button><Button
        type="submit"
        tone="info"
        surface="solid">Save pending change</Button
      >
    </div>
  </form>
</Modal>
