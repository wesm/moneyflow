<script lang="ts">
  import { Button, Modal } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'

  import type { DuplicateController } from '../../lib/controller/duplicates'
  import DeleteConfirmation from './DeleteConfirmation.svelte'

  interface Props {
    controller: DuplicateController
    onclose: () => void
  }

  let { controller, onclose }: Props = $props()
  let information = $state(false)
  const duplicateState = $derived(controller.state)
  const rows = $derived(
    (duplicateState.projection?.groups ?? []).flatMap((group) => group.rows ?? []),
  )
  const focused = $derived(rows[duplicateState.cursor])

  onMount(() => {
    if (duplicateState.phase === 'idle') void controller.open()
  })

  function close(): void {
    controller.close()
    onclose()
  }

  function handleKeydown(event: KeyboardEvent): void {
    if (
      duplicateState.phase === 'confirming' ||
      event.repeat ||
      event.altKey ||
      event.ctrlKey ||
      event.metaKey
    ) {
      return
    }
    if (
      event.key !== 'Escape' &&
      (event.key === 'Enter' || event.key === ' ') &&
      event.target instanceof HTMLButtonElement
    ) {
      return
    }
    if (information) {
      if (event.key === 'Escape' || event.key === 'Enter' || event.key === 'i') {
        event.preventDefault()
        information = false
      }
      return
    }
    let handled = true
    if (event.key === 'ArrowUp' || event.key === 'k') controller.move(-1)
    else if (event.key === 'ArrowDown' || event.key === 'j') controller.move(1)
    else if (event.key === 'Home') controller.home()
    else if (event.key === 'End') controller.end()
    else if (event.key === 'PageUp') void controller.page(-1)
    else if (event.key === 'PageDown') void controller.page(1)
    else if (event.key === ' ') void controller.toggleFocused()
    else if (event.key === 'i' || event.key === 'Enter') information = true
    else if (event.key === 'h') void controller.hideFocused()
    else if (event.key === 'x') controller.requestDelete()
    else if (event.key === 'Escape') close()
    else handled = false
    if (handled) event.preventDefault()
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if duplicateState.phase === 'confirming'}
  <DeleteConfirmation
    count={duplicateState.confirmationCount}
    submitting={false}
    onconfirm={() => void controller.confirmDelete()}
    oncancel={() => controller.cancelDelete()}
  />
{:else}
  <Modal title="Duplicate transactions" closeLabel="Close duplicate review" onclose={close}>
    <p>
      {duplicateState.projection?.total_groups ?? 0} duplicate
      {(duplicateState.projection?.total_groups ?? 0) === 1 ? 'group' : 'groups'} ·
      {duplicateState.projection?.total_transactions ?? 0} transactions
    </p>
    {#if information && focused}
      <section aria-label="Transaction information">
        <h2>Transaction information</h2>
        <dl>
          <dt>Date</dt>
          <dd>{focused.date}</dd>
          <dt>Merchant</dt>
          <dd>{focused.merchant}</dd>
          <dt>Category</dt>
          <dd>{focused.category}</dd>
          <dt>Account</dt>
          <dd>{focused.account}</dd>
          <dt>Amount</dt>
          <dd>{focused.amount.display}</dd>
        </dl>
        <Button type="button" onclick={() => (information = false)}>Back to duplicates</Button>
      </section>
    {:else}
      <div
        class="duplicate-grid"
        role="grid"
        aria-label="Likely duplicate transactions"
        tabindex="0"
      >
        <div class="duplicate-grid__header" role="row">
          <span role="columnheader">Group</span><span role="columnheader">Date</span><span
            role="columnheader">Merchant</span
          ><span role="columnheader">Category</span><span role="columnheader">Account</span><span
            role="columnheader">Amount</span
          >
        </div>
        {#each rows as row, index (row.target.identity)}
          <div
            class:duplicate-grid__row--focused={index === duplicateState.cursor}
            class="duplicate-grid__row"
            role="row"
            tabindex="-1"
            aria-selected={row.flags.selected}
            onclick={() => controller.focus(index)}
            ondblclick={() => (information = true)}
            onkeydown={(event) => {
              if (event.key !== 'Enter') return
              event.preventDefault()
              event.stopPropagation()
              information = true
            }}
          >
            <span role="gridcell">{row.group_number}</span><span role="gridcell">{row.date}</span
            ><span role="gridcell">{row.matching_label}</span><span role="gridcell"
              >{row.category}</span
            ><span role="gridcell">{row.account}</span><span role="gridcell"
              >{row.amount.display}</span
            >
          </div>
        {/each}
      </div>
      {#if rows.length === 0}<p>
          {duplicateState.projection?.status ?? 'No duplicate transactions remain.'}
        </p>{/if}
      <div class="editing-actions">
        <Button type="button" onclick={() => void controller.toggleFocused()}>Select</Button><Button
          type="button"
          onclick={() => void controller.hideFocused()}>Hide</Button
        ><Button type="button" tone="danger" onclick={() => controller.requestDelete()}
          >Delete</Button
        ><Button type="button" onclick={close}>Close</Button>
      </div>
    {/if}
    {#if duplicateState.announcement}<p role="status" aria-live="polite">
        {duplicateState.announcement}
      </p>{/if}
  </Modal>
{/if}

<style>
  .duplicate-grid {
    max-block-size: min(56vh, 34rem);
    overflow: auto;
  }
  .duplicate-grid__header,
  .duplicate-grid__row {
    display: grid;
    grid-template-columns: 4rem 7rem minmax(10rem, 1.4fr) minmax(9rem, 1fr) minmax(9rem, 1fr) 7rem;
    inline-size: 100%;
    gap: var(--kit-space-2);
    align-items: center;
    text-align: start;
  }
  .duplicate-grid__header {
    font-weight: 700;
    padding: var(--kit-space-2);
  }
  .duplicate-grid__row {
    border: 0;
    border-block-start: 1px solid var(--kit-border-subtle);
    background: transparent;
    color: inherit;
    padding: var(--kit-space-2);
  }
  .duplicate-grid__row:hover,
  .duplicate-grid__row--focused {
    background: var(--kit-surface-raised);
  }
  dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--kit-space-2) var(--kit-space-4);
  }
  dt {
    font-weight: 700;
  }
  dd {
    margin: 0;
  }
  @media (max-width: 760px) {
    .duplicate-grid {
      overflow-x: auto;
    }
    .duplicate-grid__header,
    .duplicate-grid__row {
      min-inline-size: 54rem;
    }
  }
</style>
