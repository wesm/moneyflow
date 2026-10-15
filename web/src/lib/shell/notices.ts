import { showFlash } from '@kenn-io/kit-ui'

import { MoneyflowProblem, type Problem } from '../api/client'
import { status } from './status.svelte'

/** A failure banner stays long enough to read a full sentence; the user can dismiss it sooner. */
const FAILURE_MS = 12_000
/** The server logs the cause of an unexpected error; the browser only says where to look. */
const SERVER_LOG_HINT = 'The terminal running moneyflow web shows the cause.'

/** Why a request that never reached the server failed. */
export const NETWORK_FAILURE =
  'Moneyflow could not reach the server. Check the connection and retry.'
/** Why a request whose answer was not a Moneyflow response failed. */
export const INVALID_RESPONSE = 'The server sent a response Moneyflow could not read.'

/** Shows why an action or a background operation failed, in a danger-toned banner. */
export function showFailure(message: string): void {
  showFlash(message, { tone: 'danger', durationMs: FAILURE_MS })
}

/**
 * Shows in the status bar that an operation finished, a change the user did not ask for, such as
 * a cleared selection, or an action that had nothing to do. It replaces any earlier notice.
 */
export function showNotice(message: string): void {
  status.notify(message)
}

/** The server's reason for `problem`; an unexpected server error points to the server log. */
export function problemReason(problem: Pick<Problem, 'code' | 'detail'>): string {
  return problem.code === 'internal_error' ? `${problem.detail} ${SERVER_LOG_HINT}` : problem.detail
}

/** Why a request failed: the server's reason, a connection failure, or an unreadable answer. */
export function failureReason(error: unknown): string {
  if (error instanceof MoneyflowProblem) return problemReason(error.problem)
  return error instanceof TypeError ? NETWORK_FAILURE : INVALID_RESPONSE
}
