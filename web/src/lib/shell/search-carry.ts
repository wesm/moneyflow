import type { SearchCoordinator } from '../controller/search'

/**
 * Keeps an unfinished search, with the text typed into it, between one search field and the
 * next. The command bar replaces its field when the window crosses the phone width.
 */
export class SearchCarry {
  #kept: { search: SearchCoordinator; draft: string | undefined } | undefined

  /** Stores `search` for the next field, ending any search stored before it. */
  keep(search: SearchCoordinator, draft: string | undefined): void {
    this.#kept?.search.destroy()
    this.#kept = { search, draft }
  }

  /** Returns the stored search and forgets it. */
  take(): { search: SearchCoordinator; draft: string | undefined } | undefined {
    const kept = this.#kept
    this.#kept = undefined
    return kept
  }

  /** Ends a stored search that no field took. */
  destroy(): void {
    this.#kept?.search.destroy()
    this.#kept = undefined
  }
}
