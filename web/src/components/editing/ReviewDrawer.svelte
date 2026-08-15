<script lang="ts">
  import { Button, DetailDrawer } from '@kenn-io/kit-ui'
  import { onMount } from 'svelte'
  import type { EditingController } from '../../lib/controller/editing'
  import type { ReviewController } from '../../lib/controller/review'

  interface Props {
    editing: EditingController
    review: ReviewController
    onclose: () => void
  }
  let { editing, review, onclose }: Props = $props()
  let expanded = $state('')
  let targetOffset = $state(0)
  onMount(() => {
    void review.load()
  })
  async function expand(operationID: string): Promise<void> {
    expanded = expanded === operationID ? '' : operationID
    targetOffset = 0
    if (expanded) await review.loadTargets(expanded, targetOffset)
  }
  async function pageTargets(offset: number): Promise<void> {
    if (!expanded || offset < 0) return
    if (await review.loadTargets(expanded, offset)) targetOffset = offset
  }
  async function commit(): Promise<void> {
    const revision = review.state.reviewedRevision
    if (revision === undefined) return
    if (await editing.commit(revision)) {
      review.clear()
      onclose()
    }
  }
</script>

<DetailDrawer
  title="Review pending changes"
  ariaLabel="Review pending changes"
  {onclose}
  width="min(680px, 100vw)"
>
  {#if review.state.phase === 'loading' && review.state.reviewedRevision === undefined}<p>
      Loading pending changes…
    </p>{/if}
  {#if review.state.announcement}<p role="alert">{review.state.announcement}</p>{/if}
  <section aria-labelledby="active-review-heading">
    <h2 id="active-review-heading">Active operations</h2>
    <ol class="review-list">
      {#each review.state.activeOperations as operation (operation.operation_id)}
        <li>
          <button
            type="button"
            aria-expanded={expanded === operation.operation_id}
            onclick={() => void expand(operation.operation_id)}
            ><strong>{operation.type}</strong> · {operation.affected_count} affected{#if operation.before || operation.after}<span
                >{operation.before ?? '—'} → {operation.after ?? '—'}</span
              >{/if}</button
          >
          {#if expanded === operation.operation_id}<ul>
              {#each review.targets(operation.operation_id, targetOffset) as target, index (`${targetOffset}-${index}`)}<li
                >
                  {target.date} · {target.merchant} · {target.category}{target.hidden
                    ? ' · hidden'
                    : ''}
                </li>{/each}
            </ul>
            <div class="review-paging" aria-label="Affected transaction pages">
              <Button
                size="sm"
                disabled={targetOffset === 0 || review.state.phase === 'loading'}
                onclick={() => void pageTargets(Math.max(0, targetOffset - 100))}>Previous</Button
              ><span
                >Showing {targetOffset + 1}–{Math.min(targetOffset + 100, operation.affected_count)} of
                {operation.affected_count}</span
              ><Button
                size="sm"
                disabled={review.state.phase === 'loading' ||
                  targetOffset + 100 >= operation.affected_count}
                onclick={() => void pageTargets(targetOffset + 100)}>Next</Button
              >
            </div>{/if}
        </li>{/each}
    </ol>
  </section>
  {#if review.state.inactiveOperations.length}<section
      class="review-redo"
      aria-labelledby="redo-review-heading"
    >
      <h2 id="redo-review-heading">Inactive redo operations</h2>
      <p>Commit permanently discards this redo history.</p>
      <ol>
        {#each review.state.inactiveOperations as operation (operation.operation_id)}<li>
            {operation.type} · {operation.affected_count} affected
          </li>{/each}
      </ol>
    </section>{/if}
  <div class="editing-actions">
    <Button onclick={onclose}>Cancel</Button><Button
      tone="success"
      surface="solid"
      disabled={review.state.phase === 'loading' || review.state.reviewedRevision === undefined}
      onclick={() => void commit()}>Commit reviewed changes</Button
    >
  </div>
</DetailDrawer>
