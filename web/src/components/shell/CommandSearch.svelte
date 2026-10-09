<script lang="ts">
  import { SearchInput } from '@kenn-io/kit-ui'
  import { onDestroy } from 'svelte'

  import {
    createSearchCoordinator,
    type SearchCoordinator,
    type SearchHost,
  } from '../../lib/controller/search'
  import type { ViewController } from '../../lib/controller/view-controller.svelte'

  type SearchController = SearchHost<unknown> & Pick<ViewController, 'projection' | 'hold'>

  interface Props {
    controller: SearchController
    /** Returns focus to the grid once a search is applied or cancelled. */
    ondone: () => void
  }

  let { controller, ondone }: Props = $props()
  let wrapper = $state<HTMLDivElement>()
  /** The text being typed; undefined while the field shows the applied search. */
  let draft = $state<string | undefined>()
  let error = $state('')
  let session: { search: SearchCoordinator; unsubscribe: () => void } | undefined

  const applied = $derived(controller.projection?.view.search ?? '')

  /** Focuses the field and selects its text, for the `/` key. */
  export function focus(): void {
    const input = wrapper?.querySelector('input')
    input?.focus()
    input?.select()
  }

  function begin(): SearchCoordinator {
    if (session) return session.search
    const search = createSearchCoordinator(controller, applied)
    const unsubscribe = search.subscribe(() => (error = search.error))
    session = { search, unsubscribe }
    return search
  }

  function end(): void {
    session?.unsubscribe()
    session?.search.destroy()
    session = undefined
    draft = undefined
    error = ''
  }

  function type(next: string): void {
    draft = next
    begin().input(next)
  }

  async function commit(): Promise<void> {
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

  /** Leaving the field keeps a valid preview as an applied search and drops an invalid one. */
  async function leave(search: SearchCoordinator): Promise<void> {
    if (!(await search.commit())) search.cancel()
    if (session?.search === search) end()
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
    void controller.hold(leave(session.search))
  }

  onDestroy(() => end())
</script>

<div class="mf-command-search" bind:this={wrapper} onkeydowncapture={escape} onfocusout={focusout}>
  <SearchInput
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
