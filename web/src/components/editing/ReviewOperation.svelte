<script lang="ts">
  import { Button, Chip, Table } from '@kenn-io/kit-ui'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'

  import StagedValue from '../table/StagedValue.svelte'
  import type { components } from '../../lib/api/schema'
  import { reviewTargetPage, type ReviewController } from '../../lib/controller/review'
  import {
    reviewRowChange,
    type ReviewRowChange,
    type ReviewRowField,
  } from '../../lib/editing/review-rows'
  import { transactionCount } from '../../lib/editing/toasts'

  interface Props {
    operation: components['schemas']['ReviewOperation']
    review: ReviewController
  }

  let { operation, review }: Props = $props()
  const id = $props.id()
  const total = $derived(operation.affected_count)
  const targets = $derived(review.targets(operation.operation_id))
  const remaining = $derived(Math.max(0, total - targets.length))
  const loading = $derived(review.targetsLoading(operation.operation_id))
  let expanded = $state(true)
  let toggle = $state<HTMLButtonElement>()

  /** The new value of `field`, when `change` changes it. */
  function after(change: ReviewRowChange | undefined, field: ReviewRowField): string | undefined {
    return change?.field === field ? change.after : undefined
  }

  /** Loads the next rows. Focus moves to the toggle, as the loading line replaces Show more. */
  function showMore(): void {
    toggle?.focus()
    void review.loadMore(operation.operation_id)
  }
</script>

<li class="mf-review-op">
  <button
    bind:this={toggle}
    type="button"
    class="mf-review-op__toggle"
    aria-expanded={expanded}
    aria-controls={expanded && total ? `${id}-targets` : undefined}
    onclick={() => (expanded = !expanded)}
  >
    <ChevronRightIcon class="mf-review-op__chevron" size="14" aria-hidden="true" />
    <span class="mf-review-op__label">{operation.label}</span>
    <span class="mf-review-op__count">{transactionCount(total)}</span>
    {#if operation.before || operation.after}
      <span class="mf-review-op__change"
        >{#if operation.before}<s>{operation.before}</s>{/if} →
        <span class="mf-staged">{operation.after || '—'}</span></span
      >
    {/if}
  </button>
  {#if expanded && total}
    <div class="mf-review-op__targets" id={`${id}-targets`}>
      {#if targets.length}
        <Table ariaLabel="Affected transactions" stickyHeader={false} zebra={false} unsorted>
          {#snippet header()}
            <th scope="col">Date</th>
            <th scope="col">Merchant</th>
            <th scope="col">Category</th>
            <th scope="col" class="mf-review-op__amount">Amount</th>
          {/snippet}
          {#each targets as target, index (index)}
            {@const change = reviewRowChange(operation, target)}
            <tr>
              <td>{target.date}</td>
              <td
                ><StagedValue committed={target.merchant} staged={after(change, 'merchant')} />
                {#if change?.field === 'visibility'}<StagedValue
                    committed={change.before}
                    staged={change.after}
                  />{:else if target.hidden}<Chip size="xs" tone="muted" uppercase={false}
                    >Hidden</Chip
                  >{/if}</td
              >
              <td><StagedValue committed={target.category} staged={after(change, 'category')} /></td
              >
              <td class="mf-review-op__amount">{target.amount.display} {target.amount.currency}</td>
            </tr>
          {/each}
        </Table>
      {/if}
      {#if loading}
        <p class="mf-review-op__more mf-quiet">Loading transactions…</p>
      {:else if remaining}
        <p class="mf-review-op__more">
          <span class="mf-quiet">{transactionCount(remaining)} not shown</span>
          <Button size="sm" onclick={showMore}
            >Show {Math.min(reviewTargetPage, remaining)} more</Button
          >
        </p>
      {/if}
    </div>
  {/if}
</li>

<style>
  .mf-review-op {
    border-bottom: 1px solid var(--border-muted);
  }
  /* Sticky, so the control that collapses a long page of rows stays in reach. */
  .mf-review-op__toggle {
    position: sticky;
    top: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-2) var(--space-5);
    width: 100%;
    min-height: 44px;
    padding: var(--space-5) var(--space-4);
    color: var(--text-primary);
    background: var(--bg-surface);
    border: 0;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  /* A hover background would leave the gold new value below AA contrast. */
  .mf-review-op__toggle:hover .mf-review-op__label {
    text-decoration: underline;
  }
  .mf-review-op__toggle:focus-visible {
    outline: var(--focus-ring);
    outline-offset: -2px;
  }
  .mf-review-op__toggle :global(.mf-review-op__chevron) {
    align-self: center;
    color: var(--text-secondary);
  }
  /* Rows scrolling under the sticky toggle pass below this line. */
  .mf-review-op__toggle[aria-expanded='true'] {
    box-shadow: 0 1px var(--border-muted);
  }
  .mf-review-op__toggle[aria-expanded='true'] :global(.mf-review-op__chevron) {
    transform: rotate(90deg);
  }
  .mf-review-op__label {
    font-weight: var(--font-weight-semibold);
  }
  .mf-review-op__count {
    margin-inline-start: auto;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }
  .mf-review-op__change {
    flex-basis: 100%;
    padding-inline-start: calc(14px + var(--space-5));
    color: var(--text-secondary);
  }
  .mf-review-op__targets {
    padding: 0 var(--space-4) var(--space-5) var(--space-6);
    /* The drawer already frames the operation; a second frame touches the cell text. */
    --shadow-panel: none;
  }
  /* kit Table rules tie at two classes; the targets class out-ranks them. */
  .mf-review-op__targets :global(.kit-table) {
    font-size: var(--font-size-sm);
    font-variant-numeric: tabular-nums;
  }
  .mf-review-op__targets :global(.kit-table th) {
    color: var(--text-secondary);
    font-weight: var(--font-weight-medium);
    text-align: left;
  }
  .mf-review-op__targets :global(.kit-table :is(th, td)) {
    padding: var(--space-2) var(--space-4) var(--space-2) 0;
    border-bottom: 1px solid var(--border-muted);
  }
  /* Dates and amounts keep to one line; names wrap, even inside a long word, so a row fits. */
  .mf-review-op__targets :global(.kit-table td) {
    overflow-wrap: anywhere;
    color: inherit;
  }
  .mf-review-op__targets :global(.kit-table td:first-child),
  .mf-review-op__targets :global(.kit-table .mf-review-op__amount) {
    white-space: nowrap;
  }
  /* As the table shows amounts: right-aligned figures. */
  .mf-review-op__targets :global(.kit-table .mf-review-op__amount) {
    padding-inline-end: 0;
    text-align: right;
  }
  .mf-review-op__targets :global(.kit-table td.mf-review-op__amount) {
    font-family: var(--font-mono);
  }
  /* A hover background would leave the gold new value below AA contrast. */
  .mf-review-op__targets :global(.kit-table tbody tr:hover) {
    background: none;
  }
  /* As tall as the small button it replaces, so loading does not move the rows below. */
  .mf-review-op__more {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    min-height: 28px;
    margin: var(--space-4) 0 0;
    font-size: var(--font-size-sm);
  }
</style>
