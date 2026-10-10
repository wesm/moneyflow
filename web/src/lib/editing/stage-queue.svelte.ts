import type { EditScope } from './editor-copy'

/** The editor's text and scope when a stage was requested. */
export interface StageDraft {
  text: string
  scope: EditScope
}

/**
 * A stage the anchored editor was asked for while its choices or preview were still loading.
 * It runs once loading finishes, unless the user edited the text or scope first, or the editor
 * stopped being stageable.
 */
export class StageQueue {
  #queued = $state<StageDraft>()

  /** Queues a stage of `draft`, replacing any earlier one. */
  request(draft: StageDraft): void {
    this.#queued = { ...draft }
  }

  /**
   * Decides a queued stage against the editor's current state, and returns whether to stage now.
   * The queue keeps waiting while the draft is unchanged and stageable but still loading; any
   * other state empties it.
   *
   * @param current The editor's text and scope now.
   * @param stageable Whether the draft could be staged once loaded.
   * @param valid Whether the draft can be staged now.
   */
  settle(current: StageDraft, stageable: boolean, valid: boolean): boolean {
    const queued = this.#queued
    if (!queued) return false
    const unchanged = queued.text === current.text && queued.scope === current.scope
    if (unchanged && stageable && !valid) return false
    this.#queued = undefined
    return unchanged && stageable
  }
}
