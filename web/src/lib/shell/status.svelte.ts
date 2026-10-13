import type { ProviderWriteStatus } from '../api/client'
import type { ControllerProblem } from '../controller/view-controller.svelte'

export type Connection = 'connected' | 'updating' | 'offline' | 'failed'

export interface WriteProgress {
  completed: number
  total: number
}

/**
 * Reports the server connection: offline after a network error, failed after the server refused
 * or could not answer the last view request, and updating while loading.
 */
export function connectionState(
  problem: ControllerProblem | undefined,
  loading: boolean,
): Connection {
  if (problem?.code === 'network_error') return 'offline'
  if (problem && !loading) return 'failed'
  return loading ? 'updating' : 'connected'
}

/** Reports a provider write's progress while the write has a phase. */
export function writeProgress(status: ProviderWriteStatus | undefined): WriteProgress | undefined {
  if (!status?.phase) return undefined
  return { completed: status.completed, total: status.total }
}

/** How long the status line shows a notice. */
const NOTICE_MS = 4000

/**
 * The status line's notice slot: one short message, such as why a key did nothing. A new notice
 * replaces the last. Each clears after 4 seconds or on the next action: a shortcut, a cursor
 * move, or a view transition.
 */
class StatusNotice {
  notice = $state('')
  #timer: ReturnType<typeof setTimeout> | undefined

  notify(text: string): void {
    this.#show(text)
    this.#timer = setTimeout(() => this.#show(''), NOTICE_MS)
  }

  clear(): void {
    this.#show('')
  }

  #show(text: string): void {
    clearTimeout(this.#timer)
    this.#timer = undefined
    this.notice = text
  }
}

export const status = new StatusNotice()
