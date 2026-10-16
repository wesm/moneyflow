import { applicationURL } from './base-path'

/** Where the workspace keeps its view: the host's router, or the browser history. */
export interface ViewStateAdapter {
  /** State saved with the current history entry, or null. */
  readonly state: unknown
  /** The canonical moneyflow view query of the current entry, without "?". */
  readonly query: string
  push(state: unknown, query: string): void
  replace(state: unknown, query: string): void
  go(delta: number): void
  /** Calls restore after back or forward navigation; returns an unsubscribe function. */
  listen(restore: () => void): () => void
}

/**
 * The adapter standalone moneyflow uses: history.pushState/replaceState on
 * `${applicationPath}?${query}`.
 */
export function historyViewState(applicationPath: string): ViewStateAdapter {
  return {
    get state() {
      return history.state as unknown
    },
    get query() {
      return location.search.replace(/^\?/, '')
    },
    push(state, query) {
      history.pushState(state, '', applicationURL(applicationPath, query))
    },
    replace(state, query) {
      history.replaceState(state, '', applicationURL(applicationPath, query))
    },
    go(delta) {
      history.go(delta)
    },
    listen(restore) {
      addEventListener('popstate', restore)
      return () => removeEventListener('popstate', restore)
    },
  }
}
