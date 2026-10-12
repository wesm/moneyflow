import type { SearchCoordinator } from '../controller/search'

interface CarriedSearch {
  /** The unfinished search; undefined when the field had focus but no search was open. */
  search: SearchCoordinator | undefined
  /** The text typed into the field that stored the search. */
  draft: string | undefined
  /** Whether that field had focus, so the next field takes it. */
  focused: boolean
}

/**
 * Keeps an unfinished search, with the text typed into it, and the field's focus between one
 * search field and the next. The command bar replaces its field when the window crosses the
 * phone width.
 */
export class SearchCarry {
  #kept: CarriedSearch | undefined

  /** Stores `search` for the next field, ending any search stored before it. */
  keep(search: SearchCoordinator | undefined, draft: string | undefined, focused: boolean): void {
    this.#kept?.search?.destroy()
    this.#kept = { search, draft, focused }
  }

  /** Returns the stored search and forgets it. */
  take(): CarriedSearch | undefined {
    const kept = this.#kept
    this.#kept = undefined
    return kept
  }

  /** Ends a stored search that no field took. */
  destroy(): void {
    this.#kept?.search?.destroy()
    this.#kept = undefined
  }
}
