<script lang="ts">
  import { Button, Chip } from '@kenn-io/kit-ui'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
  import { tick } from 'svelte'

  import type { components } from '../../lib/api/schema'
  import type { ReviewController } from '../../lib/controller/review'
  import { transactionCount } from '../../lib/editing/toasts'

  interface Props {
    operation: components['schemas']['ReviewOperation']
    review: ReviewController
    expanded: boolean
    /** Expands this operation, collapsing any other, or collapses it. */
    ontoggle: () => void
  }

  let { operation, review, expanded, ontoggle }: Props = $props()
  const PAGE = 100
  const id = $props.id()
  const total = $derived(operation.affected_count)
  let offset = $state(0)
  let paging = $state<HTMLElement>()
  const targets = $derived(expanded ? review.targets(operation.operation_id, offset) : [])

  async function toggle(): Promise<void> {
    const opening = !expanded
    ontoggle()
    if (!opening) return
    offset = 0
    await review.loadTargets(operation.operation_id, 0)
  }

  /** Shows the page at `next`. At the first or last page, focus moves to the other button. */
  async function page(next: number): Promise<void> {
    if (review.state.phase === 'loading') return
    if (await review.loadTargets(operation.operation_id, next)) offset = next
    await tick()
    if (paging && !paging.contains(document.activeElement)) {
      paging.querySelector<HTMLElement>('button:enabled')?.focus()
    }
  }
</script>

<li class="mf-review-op">
  <button
    type="button"
    class="mf-review-op__toggle"
    aria-expanded={expanded}
    aria-controls={expanded ? `${id}-targets` : undefined}
    onclick={() => void toggle()}
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
  {#if expanded}
    <div class="mf-review-op__targets" id={`${id}-targets`}>
      <table class="mf-review-op__table">
        <caption class="kit-sr-only">Affected transactions</caption>
        <thead>
          <tr>
            <th scope="col">Date</th>
            <th scope="col">Merchant</th>
            <th scope="col">Category</th>
            <th scope="col" class="mf-review-op__amount">Amount</th>
          </tr>
        </thead>
        <tbody>
          {#each targets as target, index (offset + index)}
            <tr>
              <td>{target.date}</td>
              <td
                >{target.merchant}
                {#if target.hidden}<Chip size="xs" tone="muted" uppercase={false}>Hidden</Chip
                  >{/if}</td
              >
              <td>{target.category}</td>
              <td class="mf-review-op__amount">{target.amount.display} {target.amount.currency}</td>
            </tr>
          {/each}
        </tbody>
      </table>
      <div class="mf-review-op__paging" bind:this={paging}>
        <Button size="sm" disabled={offset === 0} onclick={() => void page(offset - PAGE)}
          >Previous</Button
        >
        <span
          >Showing {Math.min(offset + 1, total)}–{Math.min(offset + PAGE, total)} of {total}</span
        >
        <Button size="sm" disabled={offset + PAGE >= total} onclick={() => void page(offset + PAGE)}
          >Next</Button
        >
      </div>
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
  }
  .mf-review-op__table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--font-size-sm);
    font-variant-numeric: tabular-nums;
  }
  .mf-review-op__table th {
    color: var(--text-secondary);
    font-weight: var(--font-weight-medium);
    text-align: left;
  }
  .mf-review-op__table :is(th, td) {
    padding: var(--space-2) var(--space-4) var(--space-2) 0;
    border-bottom: 1px solid var(--border-muted);
  }
  /* Dates and amounts keep to one line; names wrap, even inside a long word, so a row fits. */
  .mf-review-op__table td {
    overflow-wrap: anywhere;
  }
  .mf-review-op__table td:first-child,
  .mf-review-op__table .mf-review-op__amount {
    white-space: nowrap;
  }
  /* As the table shows amounts: right-aligned figures. */
  .mf-review-op__table .mf-review-op__amount {
    padding-inline-end: 0;
    text-align: right;
  }
  .mf-review-op__table td.mf-review-op__amount {
    font-family: var(--font-mono);
  }
  .mf-review-op__paging {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    margin-top: var(--space-4);
    color: var(--text-secondary);
    font-size: var(--font-size-sm);
  }
</style>
