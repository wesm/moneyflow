import type { MerchantPreview } from '../api/client'
import type { EditScope } from './editor-copy'

export type PreviewFetch = (scope: EditScope, signal: AbortSignal) => Promise<MerchantPreview>

const PREVIEW_FAILED =
  'Affected transactions could not be loaded. Change scope or reopen this editor to retry.'

/** The anchored editor's affected-transaction previews, one per scope. */
export class ScopePreviews {
  byScope = $state<Partial<Record<EditScope, MerchantPreview>>>({})
  error = $state('')
  #fetch: PreviewFetch

  constructor(fetch: PreviewFetch) {
    this.#fetch = fetch
  }

  /** Each loaded scope's affected-transaction count, which labels its scope choice. */
  get counts(): Partial<Record<EditScope, number | undefined>> {
    return {
      transactions: this.byScope.transactions?.affected_transactions,
      entity: this.byScope.entity?.affected_transactions,
    }
  }

  /**
   * Requests `scope`'s preview and returns the function that aborts it. An aborted request
   * neither stores its preview nor reports an error.
   */
  load(scope: EditScope): () => void {
    const request = new AbortController()
    void this.#load(scope, request.signal)
    return () => request.abort()
  }

  async #load(scope: EditScope, signal: AbortSignal): Promise<void> {
    this.error = ''
    try {
      const result = await this.#fetch(scope, signal)
      if (!signal.aborted) this.byScope[scope] = result
    } catch {
      if (!signal.aborted) this.error = PREVIEW_FAILED
    }
  }
}
