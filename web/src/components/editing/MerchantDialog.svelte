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
  let loaded = $state(false)
  const uid = $props.id()
  let error = $state('')
  let submitting = $state(false)
  let preview = $state<MerchantPreview>()
  let previewError = $state('')
  const choices = $derived.by(() => {
    const query = label.trim().toLocaleLowerCase()
    if (!query || !loaded) return []
    const matches = merchants.filter((merchant) =>
      merchant.label.toLocaleLowerCase().includes(query),
    )
    const exact = matches.find((merchant) => merchant.label.trim().toLocaleLowerCase() === query)
    if (exact) return [exact, ...matches.filter((merchant) => merchant !== exact)]
    return [...matches, { id: '', label: label.trim() }]
  })
  // A new search resets manual selection to its first result, including after Create.
  let highlighted = $derived(choices.length ? 0 : -1)
  const choice = $derived(choices[highlighted])
  // Use TextInput's combobox contract for an inline list. Typeahead's popup would
  // cover the affected-transaction preview and selects a custom name by default.
  const suggestionAttributes = $derived({
    role: 'combobox' as const,
    ariaExpanded: choices.length > 0,
    ariaAutocomplete: 'list' as const,
    ...(choice
      ? { ariaControls: `${uid}-choices`, ariaActivedescendant: `${uid}-choice-${highlighted}` }
      : {}),
  })

  onMount(() => {
    scope = 'transactions'
    void controller
      .catalog()
      .then((catalog) => {
        merchants = catalog.merchants ?? []
        loaded = true
      })
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

  function keydown(event: KeyboardEvent): void {
    if (event.isComposing || !choices.length) return
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
    event.preventDefault()
    highlighted =
      (highlighted + (event.key === 'ArrowDown' ? 1 : -1) + choices.length) % choices.length
    document.getElementById(`${uid}-choice-${highlighted}`)?.scrollIntoView({ block: 'nearest' })
  }

  async function submit(): Promise<void> {
    if (!preview || !loaded || submitting) return
    if (BigInt(preview.revision) !== controller.state.revision) {
      error = 'The profile changed. Reopen this editor to review affected transactions.'
      return
    }
    if (!choice) {
      error = 'Enter a merchant name.'
      return
    }
    submitting = true
    const destination =
      choice.id || (scope === 'transactions' ? `merchant_${crypto.randomUUID()}` : '')
    const accepted = await controller.submit({
      action: 'transaction.edit-merchant',
      target,
      input: {
        scope,
        label: choice.label,
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
    <label for="{uid}-name">Merchant name</label>
    <TextInput
      id="{uid}-name"
      bind:value={label}
      block
      autofocus
      autocomplete="off"
      invalid={!!error}
      {...suggestionAttributes}
      onkeydown={keydown}
    />
    {#if choices.length}
      <!-- kit-ui-check-ignore: inline list for the merchant field, not a separate popup picker -->
      <div class="merchant-choices" role="listbox" id="{uid}-choices" aria-label="Merchants">
        {#each choices as candidate, index (candidate.id)}
          <!-- kit-ui-check-ignore: listbox options need option roles and active-descendant focus -->
          <button
            type="button"
            role="option"
            id="{uid}-choice-{index}"
            aria-selected={index === highlighted}
            tabindex="-1"
            onclick={() => {
              highlighted = index
              document.getElementById(`${uid}-name`)?.focus()
            }}
          >
            {candidate.id
              ? candidate.label
              : `${scope === 'entity' ? 'Rename to' : 'Create'} “${candidate.label}”`}
          </button>
        {/each}
      </div>
    {/if}
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
    {#if choice?.id && scope === 'entity'}<p role="status">Merge into {choice.label}.</p>{/if}
    {#if error}<p class="editing-error" role="alert">{error}</p>{/if}
    <div class="editing-actions">
      <Button type="button" onclick={onclose}>Cancel</Button><Button
        type="submit"
        tone="info"
        surface="solid"
        disabled={submitting || !preview || !loaded || !choice}>Save pending change</Button
      >
    </div>
  </form>
</Modal>

<style>
  .merchant-choices {
    max-height: 10rem;
    overflow-y: auto;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
  }
  .merchant-choices button {
    display: block;
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: 0;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: left;
    overflow-wrap: anywhere;
    cursor: pointer;
  }
  .merchant-choices button:hover {
    background: var(--bg-surface-hover);
  }
  .merchant-choices button[aria-selected='true'] {
    background: color-mix(in srgb, var(--accent-blue) 12%, transparent);
    box-shadow: inset 3px 0 var(--accent-blue);
  }
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
