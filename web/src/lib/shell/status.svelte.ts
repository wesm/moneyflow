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

/**
 * The status line's notice slot: one short message, such as a finished refresh, a warning, or why
 * a key did nothing. A notice stays until a newer one replaces it or the user's next action
 * clears it: a shortcut, a cursor move, or a view transition. A provider write that starts also
 * clears it, so the write's progress shows.
 */
class StatusNotice {
  notice = $state('')

  notify(text: string): void {
    this.notice = text
  }

  clear(): void {
    this.notice = ''
  }
}

export const status = new StatusNotice()
