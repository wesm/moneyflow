import type { ProviderWriteStatus } from '../api/client'
import type { ControllerProblem } from '../controller/view-controller.svelte'

export type Connection = 'connected' | 'updating' | 'offline'

export interface WriteProgress {
  completed: number
  total: number
}

/** Reports the server connection: offline after a network error, updating while loading. */
export function connectionState(
  problem: ControllerProblem | undefined,
  loading: boolean,
): Connection {
  if (problem?.code === 'network_error') return 'offline'
  return loading ? 'updating' : 'connected'
}

/** Reports a provider write's progress while the write has a phase. */
export function writeProgress(status: ProviderWriteStatus | undefined): WriteProgress | undefined {
  if (!status?.phase) return undefined
  return { completed: status.completed, total: status.total }
}
