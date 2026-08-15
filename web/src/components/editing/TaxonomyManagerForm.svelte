<script lang="ts">
  import { Button, Modal, SearchInput, TextInput } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'
  import type { EditorCatalog } from '../../lib/api/client'
  import type { EditingController } from '../../lib/controller/editing'

  interface Props {
    kind: 'category' | 'group'
    controller: EditingController
    onclose: () => void
  }
  let { kind, controller, onclose }: Props = $props()
  let catalog = $state<EditorCatalog | undefined>()
  let operation = $state('create')
  let source = $state('')
  let destination = $state('')
  let label = $state('')
  let filter = $state('')
  let error = $state('')
  const sources = $derived(
    kind === 'category' ? (catalog?.categories ?? []) : (catalog?.groups ?? []),
  )
  const destinations = $derived(operation === 'move' ? (catalog?.groups ?? []) : sources)
  const filteredSources = $derived(
    sources.filter((item) => item.label.toLocaleLowerCase().includes(filter.toLocaleLowerCase())),
  )
  onMount(() => {
    void controller
      .catalog()
      .then((value) => {
        catalog = value
        source =
          (kind === 'category' ? value.categories : value.groups)?.find((item) => !item.protected)
            ?.id ?? ''
        destination = value.groups?.find((item) => !item.protected)?.id ?? ''
      })
      .catch(() => (error = 'Taxonomy choices could not be loaded.'))
  })

  function changeOperation(next: string): void {
    operation = next
    destination =
      next === 'move' || (next === 'create' && kind === 'category')
        ? (catalog?.groups?.find((item) => !item.protected)?.id ?? '')
        : (sources.find((item) => !item.protected && item.id !== source)?.id ?? '')
  }

  async function submit(): Promise<void> {
    if ((operation === 'create' || operation === 'rename') && !label.trim()) {
      error = 'Enter a name.'
      return
    }
    if (operation !== 'create' && !source) {
      error = `Choose a ${kind}.`
      return
    }
    if (['move', 'merge', 'delete'].includes(operation) && !destination) {
      error = 'Choose an explicit destination.'
      return
    }
    const entityID = operation === 'create' ? `${kind}_${crypto.randomUUID()}` : source
    const accepted = await controller.submit({
      action: kind === 'category' ? 'category.manage' : 'category-group.manage',
      input: {
        taxonomy: operation,
        entity_id: entityID,
        ...(label.trim() ? { label: label.trim() } : {}),
        ...(operation === 'create' && kind === 'category' ? { group_id: destination } : {}),
        ...(operation === 'move' || operation === 'merge' ? { destination_id: destination } : {}),
        ...(operation === 'delete' ? { replacement_id: destination } : {}),
      },
    })
    if (accepted) onclose()
    else error = controller.state.announcement
  }
</script>

<Modal
  title={kind === 'category' ? 'Manage categories' : 'Manage category groups'}
  closeLabel="Close manager"
  {onclose}
  maxWidth="min(640px, calc(100vw - 24px))"
>
  <form
    class="editing-form"
    onsubmit={(event) => {
      event.preventDefault()
      void submit()
    }}
  >
    <label for={`${kind}-operation`}>Operation</label>
    <select
      id={`${kind}-operation`}
      value={operation}
      onchange={(event) => changeOperation(event.currentTarget.value)}
    >
      <option value="create">Create</option><option value="rename">Rename</option>
      {#if kind === 'category'}<option value="move">Move</option>{/if}
      <option value="merge">Merge</option><option value="delete">Delete</option>
    </select>
    {#if operation !== 'create'}
      <label for={`${kind}-filter`}>Filter {kind === 'category' ? 'categories' : 'groups'}</label>
      <SearchInput
        id={`${kind}-filter`}
        value={filter}
        block
        ariaLabel={`Filter ${kind === 'category' ? 'categories' : 'groups'}`}
        oninput={(value) => (filter = value)}
      />
      <label for={`${kind}-source`}>{kind === 'category' ? 'Category' : 'Group'}</label>
      <select id={`${kind}-source`} bind:value={source}
        >{#each filteredSources as item (item.id)}<option value={item.id} disabled={item.protected}
            >{item.label}{item.protected ? ' (protected)' : ''}</option
          >{/each}</select
      >
    {/if}
    {#if operation === 'create' || operation === 'rename'}
      <label for={`${kind}-label`}>Name</label><TextInput
        id={`${kind}-label`}
        bind:value={label}
        block
        autofocus
      />
    {/if}
    {#if (operation === 'create' && kind === 'category') || ['move', 'merge', 'delete'].includes(operation)}
      <label for={`${kind}-destination`}
        >{operation === 'move' || operation === 'create' ? 'Group' : 'Destination'}</label
      >
      <select id={`${kind}-destination`} bind:value={destination}
        >{#each destinations as item (item.id)}<option value={item.id} disabled={item.id === source}
            >{item.label}</option
          >{/each}</select
      >
      {#if operation === 'merge' || operation === 'delete'}<p>
          Transactions and taxonomy references will move to the explicit destination.
        </p>{/if}
    {/if}
    {#if error}<p class="editing-error" role="alert">{error}</p>{/if}
    <div class="editing-actions">
      <Button type="button" onclick={onclose}>Cancel</Button><Button
        type="submit"
        tone={operation === 'delete' ? 'danger' : 'info'}
        surface="solid">Save pending operation</Button
      >
    </div>
  </form>
</Modal>
