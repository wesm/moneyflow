<script lang="ts">
  import { Button, Modal } from '@kenn-io/kit-ui'

  interface Props {
    count: number
    onconfirm: () => void
    oncancel: () => void
    submitting?: boolean
  }

  let { count, onconfirm, oncancel, submitting = false }: Props = $props()

  function handleKeydown(event: KeyboardEvent): void {
    if (submitting || event.repeat || event.altKey || event.ctrlKey || event.metaKey) return
    if (event.target instanceof HTMLButtonElement) return
    if (event.key === 'Enter') {
      event.preventDefault()
      onconfirm()
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<Modal
  title="Confirm deletion"
  closeLabel="Cancel deletion"
  onclose={() => {
    if (!submitting) oncancel()
  }}
>
  <p>Delete {count} {count === 1 ? 'transaction' : 'transactions'}?</p>
  <p>This stages a pending edit; nothing reaches the provider until review and commit.</p>
  <div class="editing-actions">
    <Button type="button" disabled={submitting} onclick={oncancel}>Cancel</Button><Button
      type="button"
      tone="danger"
      surface="solid"
      disabled={submitting}
      onclick={onconfirm}>Stage deletion</Button
    >
  </div>
</Modal>
