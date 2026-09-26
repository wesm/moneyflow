<script lang="ts">
  import { Button, Modal, SelectDropdown, TextInput } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'
  import type { EditingController } from '../../lib/controller/editing'
  import type { MerchantPreview } from '../../lib/api/client'

  interface Props {
    controller: EditingController
    target: { kind: string; identity: string }
    hasSelection: boolean
    onclose: () => void
  }
  let { controller, target, hasSelection, onclose }: Props = $props()
  let label = $state('')
  let scope = $state('')
  let merchants = $state<Array<{ id: string; label: string }>>([])
  let error = $state('')
  let submitting = $state(false)
  let preview = $state<MerchantPreview>()
  let previewError = $state('')
  const collision = $derived(
    merchants.find(
      (merchant) => merchant.label.trim().toLocaleLowerCase() === label.trim().toLocaleLowerCase(),
    ),
  )

  onMount(() => {
    scope = hasSelection ? 'transactions' : 'entity'
    void controller
      .catalog()
      .then((catalog) => (merchants = catalog.merchants ?? []))
      .catch(() => (error = 'Merchant choices could not be loaded.'))
  })

  $effect(() => {
    if (!scope) return
    const request = new AbortController()
    preview = undefined
    previewError = ''
    void controller
      .previewMerchant(target, scope, request.signal)
      .then((result) => {
        if (!request.signal.aborted) preview = result
      })
      .catch(() => {
        if (!request.signal.aborted)
          previewError =
            'Affected transactions could not be loaded. Change scope or reopen this editor to retry.'
      })
    return () => request.abort()
  })

  async function submit(): Promise<void> {
    if (!preview) return
    if (BigInt(preview.revision) !== controller.state.revision) {
      error = 'The profile changed. Reopen this editor to review affected transactions.'
      return
    }
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
          label: hasSelection ? 'Selected transactions' : 'Transactions in focused row',
        },
      ]}
      onchange={(value) => (scope = value)}
    />
    {#if preview}
      <section class="merchant-preview" aria-label="Affected transactions">
        <p class="affected-count">
          {preview.affected_transactions}
          {preview.affected_transactions === 1 ? 'transaction' : 'transactions'} affected
        </p>
        {#if scope === 'entity'}<p>Includes transactions outside current filters.</p>{/if}
        <div class="preview-table">
          <table>
            <caption
              >Showing {preview.transactions?.length ?? 0} of {preview.affected_transactions} transactions</caption
            >
            <thead
              ><tr
                ><th scope="col">Date</th><th scope="col">Merchant / category</th><th
                  scope="col"
                  class="amount">Amount</th
                ></tr
              ></thead
            >
            <tbody>
              {#each preview.transactions ?? [] as transaction (transaction.identity)}
                <tr
                  ><td>{transaction.date}</td><td
                    >{transaction.merchant}<span
                      >{transaction.category} · {transaction.account}</span
                    ></td
                  ><td class="amount">{transaction.amount.display} {transaction.amount.currency}</td
                  ></tr
                >
              {/each}
            </tbody>
          </table>
        </div>
      </section>
    {:else if previewError}
      <p class="editing-error" role="alert">{previewError}</p>
    {:else}
      <p>Loading affected transactions…</p>
    {/if}
    {#if collision}<p role="status">This will merge or reassign into {collision.label}.</p>{/if}
    {#if error}<p class="editing-error" role="alert">{error}</p>{/if}
    <div class="editing-actions">
      <Button type="button" onclick={onclose}>Cancel</Button><Button
        type="submit"
        tone="info"
        surface="solid"
        disabled={submitting || !preview}>Save pending change</Button
      >
    </div>
  </form>
</Modal>

<style>
  .merchant-preview {
    min-width: 0;
  }
  .merchant-preview p {
    margin: 0 0 var(--space-2);
  }
  .affected-count {
    font-weight: 600;
  }
  .preview-table {
    overflow-x: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--font-size-sm);
  }
  caption {
    text-align: left;
    padding-block: var(--space-2);
    color: var(--text-secondary);
  }
  th,
  td {
    text-align: left;
    vertical-align: top;
    padding: var(--space-2);
    border-bottom: 1px solid var(--border-default);
  }
  th:first-child,
  td:first-child {
    padding-left: 0;
    white-space: nowrap;
  }
  td span {
    display: block;
    color: var(--text-secondary);
    font-size: var(--font-size-xs);
  }
  .amount {
    text-align: right;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
</style>
