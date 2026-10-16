<script lang="ts">
  import { SearchInput } from '@kenn-io/kit-ui'
  import { onDestroy, onMount, tick } from 'svelte'

  import {
    createSearchCoordinator,
    type SearchCoordinator,
    type SearchHost,
  } from '../../lib/controller/search'
  import type { ViewController } from '../../lib/controller/view-controller.svelte'
  import type { SearchCarry } from '../../lib/shell/search-carry'

  type SearchController = SearchHost<unknown> &
    Pick<ViewController, 'projection' | 'hold' | 'opening' | 'settled' | 'viewState'>

  interface Props {
    controller: SearchController
    /** Returns focus to the grid once a search is applied or cancelled. */
    ondone: () => void
    /**
     * Holds an unfinished search while the field is replaced, such as when the window crosses
     * the phone width, so the next field continues it. Its owner ends a search left in it.
     */
    carry?: SearchCarry | undefined
    /** Shows the field, for a carried search that had focus to take it again. */
    reveal?: () => void
    /** Settles once view changes queued before typing finish; the controller's by default. */
    settled?: () => Promise<void>
    /** Whether the shell is gone; a controller the host keeps outlives it. */
    stopped?: () => boolean
  }

  let {
    controller,
    ondone,
    carry,
    reveal,
    settled = () => controller.settled(),
    stopped = () => false,
  }: Props = $props()
  let wrapper = $state<HTMLDivElement>()
  // kit types `inputEl` without `| undefined`, so the binding below casts the unmounted field.
  let inputEl = $state<HTMLInputElement>()
  /** The text being typed; undefined while the field shows the applied search. */
  let draft = $state<string | undefined>()
  let error = $state('')
  let session: { search: SearchCoordinator; unsubscribe: () => void } | undefined
  /** A search that lost focus and is still being applied; new typing waits for it. */
  let leaving: Promise<void> | undefined
  /** The search `leaving` applies, so browser history can end it. */
  let leavingSearch: SearchCoordinator | undefined
  /** View changes queued before the first keystroke; the search starts from their result. */
  let waiting: Promise<void> | undefined
  /** Whether the field has focus. It stays set when the field is removed with focus. */
  let focused = false

  const applied = $derived(controller.projection?.view.search ?? '')

  /** Focuses the field and selects its text, for the `/` key. */
  export function focus(): void {
    inputEl?.focus()
    inputEl?.select()
  }

  /**
   * The controller as a search uses it. A search applied once the shell is gone, such as one
   * whose field lost focus just before, restores the view it started from instead.
   */
  const host: SearchHost<unknown> = {
    beginSearch: () => controller.beginSearch(),
    previewSearch: (search) => controller.previewSearch(search),
    commitSearch: (snapshot) =>
      stopped() ? controller.restoreSearch(snapshot) : controller.commitSearch(snapshot),
    restoreSearch: (snapshot) => controller.restoreSearch(snapshot),
  }

  function begin(): SearchCoordinator {
    if (session) return session.search
    session = attach(createSearchCoordinator(host, applied))
    return session.search
  }

  function attach(search: SearchCoordinator) {
    return { search, unsubscribe: search.subscribe(() => (error = search.error)) }
  }

  /**
   * Continues a search that an earlier field left in `carry`. Svelte creates a replacing
   * field before it destroys the old one, so this waits until mount, after the old field
   * stored its search and focus.
   */
  function resume() {
    const carried = carry?.take()
    if (carried?.focused) void refocus()
    if (!carried?.search) return undefined
    draft = carried.draft
    error = carried.search.error
    return attach(carried.search)
  }

  /** Focuses the field with the caret after the carried text, so typing continues there. */
  async function refocus(): Promise<void> {
    reveal?.()
    await tick()
    inputEl?.focus()
    inputEl?.setSelectionRange(inputEl.value.length, inputEl.value.length)
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
    if (leaving || waiting) return
    if (session) return session.search.input(next)
    waiting = settled().then(() => {
      waiting = undefined
      if (draft !== undefined && wrapper?.isConnected) begin().input(draft)
    })
  }

  async function commit(): Promise<void> {
    await leaving
    if (waiting) await waiting
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
    leavingSearch = undefined
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
    if (next instanceof Node && wrapper?.contains(next)) return
    // Switching windows blurs the field too; the search stays open for the user's return.
    if (!document.hasFocus()) return
    // Chromium also blurs a field as it removes it, before it is detached. A field replaced at
    // the phone width hands its search and focus to the next field instead of leaving.
    queueMicrotask(() => {
      if (wrapper?.isConnected) left()
    })
  }

  function left(): void {
    focused = false
    // Text typed while earlier view changes load has no search yet; nothing would apply it.
    if (waiting) draft = undefined
    if (!session) return
    leavingSearch = detach()
    if (leavingSearch) leaving = controller.hold(leave(leavingSearch, draft))
  }

  // Back or forward replaced the view and dropped this search's starting point.
  function historyChanged(): void {
    leavingSearch?.destroy()
    if (session) end()
  }

  $effect(() => controller.viewState.listen(historyChanged))

  onMount(() => {
    session = resume()
  })

  onDestroy(() => {
    const search = detach()
    if (carry && (search || focused)) carry.keep(search, draft, focused)
    else search?.destroy()
  })
</script>

<div
  class="mf-command-search"
  bind:this={wrapper}
  onkeydowncapture={escape}
  onfocusin={() => (focused = true)}
  onfocusout={focusout}
>
  <SearchInput
    bind:inputEl={() => inputEl as HTMLInputElement, (element) => (inputEl = element)}
    value={draft ?? applied}
    size="sm"
    disabled={controller.opening}
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
