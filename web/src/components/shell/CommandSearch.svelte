<script lang="ts">
  import { SearchInput } from '@kenn-io/kit-ui'
  import { onDestroy } from 'svelte'

  import {
    createSearchCoordinator,
    type SearchCoordinator,
    type SearchHost,
  } from '../../lib/controller/search'
  import type { ViewController } from '../../lib/controller/view-controller.svelte'
  import type { SearchCarry } from '../../lib/shell/search-carry'

  type SearchController = SearchHost<unknown> & Pick<ViewController, 'projection' | 'hold'>

  interface Props {
    controller: SearchController
    /** Returns focus to the grid once a search is applied or cancelled. */
    ondone: () => void
    /**
     * Holds an unfinished search while the field is replaced, such as when the window crosses
     * the phone width, so the next field continues it. Its owner ends a search left in it.
     */
    carry?: SearchCarry | undefined
  }

  let { controller, ondone, carry }: Props = $props()
  let wrapper = $state<HTMLDivElement>()
  // kit types `inputEl` without `| undefined`, so the binding below casts the unmounted field.
  let inputEl = $state<HTMLInputElement>()
  /** The text being typed; undefined while the field shows the applied search. */
  let draft = $state<string | undefined>()
  let error = $state('')
  let session: { search: SearchCoordinator; unsubscribe: () => void } | undefined = resume()
  /** A search that lost focus and is still being applied; new typing waits for it. */
  let leaving: Promise<void> | undefined

  const applied = $derived(controller.projection?.view.search ?? '')

  /** Focuses the field and selects its text, for the `/` key. */
  export function focus(): void {
    inputEl?.focus()
    inputEl?.select()
  }

  function begin(): SearchCoordinator {
    if (session) return session.search
    session = attach(createSearchCoordinator(controller, applied))
    return session.search
  }

  function attach(search: SearchCoordinator) {
    return { search, unsubscribe: search.subscribe(() => (error = search.error)) }
  }

  /** Continues a search that an earlier field left in `carry`. */
  function resume() {
    const carried = carry?.take()
    if (!carried) return undefined
    draft = carried.draft
    error = carried.search.error
    return attach(carried.search)
  }

  /** Detaches the session from the field without restoring or applying it. */
  function detach(): SearchCoordinator | undefined {
    const search = session?.search
    session?.unsubscribe()
    session = undefined
    return search
  }

  function end(): void {
    detach()?.destroy()
    draft = undefined
    error = ''
  }

  function type(next: string): void {
    draft = next
    if (!leaving) begin().input(next)
  }

  async function commit(): Promise<void> {
    await leaving
    const search = session?.search
    if (search && !(await search.commit())) return
    end()
    ondone()
  }

  function cancel(): void {
    session?.search.cancel()
    end()
    ondone()
  }

  /**
   * Leaving the field keeps a valid preview as an applied search and drops an invalid one.
   * Text typed meanwhile starts a new search once this one is settled, but only while the
   * field still has focus: nothing would apply or end a search in a field already left.
   */
  async function leave(search: SearchCoordinator, typed: string | undefined): Promise<void> {
    if (!(await search.commit())) search.cancel()
    search.destroy()
    leaving = undefined
    if (draft !== undefined && draft !== typed && document.activeElement === inputEl) {
      begin().input(draft)
      return
    }
    draft = undefined
    error = ''
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key !== 'Enter') return
    event.preventDefault()
    void commit()
  }

  // kit SearchInput clears its text on Escape; Esc here restores the applied search instead.
  function escape(event: KeyboardEvent): void {
    if (event.key !== 'Escape') return
    event.preventDefault()
    event.stopPropagation()
    cancel()
  }

  function focusout(event: FocusEvent): void {
    const next = event.relatedTarget
    if (!session || (next instanceof Node && wrapper?.contains(next))) return
    // Switching windows blurs the field too; the search stays open for the user's return.
    if (!document.hasFocus()) return
    const search = detach()
    if (search) leaving = controller.hold(leave(search, draft))
  }

  // Browser history replaced the view and dropped this search's starting point.
  function historyChanged(): void {
    if (session) end()
  }

  onDestroy(() => {
    const search = detach()
    if (search && carry) carry.keep(search, draft)
    else search?.destroy()
  })
</script>

<svelte:window onpopstate={historyChanged} />

<div class="mf-command-search" bind:this={wrapper} onkeydowncapture={escape} onfocusout={focusout}>
  <SearchInput
    bind:inputEl={() => inputEl as HTMLInputElement, (element) => (inputEl = element)}
    value={draft ?? applied}
    size="sm"
    keys={['/']}
    invalid={error !== ''}
    ariaLabel="Search transactions"
    placeholder="Search"
    oninput={type}
    onkeydown={keydown}
  />
  <div class="mf-command-search__status" aria-live="polite">
    {#if error}<p class="mf-command-search__error kit-popover-card">{error}</p>{/if}
  </div>
</div>
