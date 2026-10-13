import { showFlash } from '@kenn-io/kit-ui'

import type { Problem } from '../api/client'

/** A banner stays long enough to read a full sentence; the user can dismiss it sooner. */
const NOTICE_MS = 12_000
/** The server logs the cause of an unexpected error; the browser only says where to look. */
const SERVER_LOG_HINT = 'The terminal running moneyflow web shows the cause.'

/** Shows why an action or a background operation failed, in a danger-toned banner. */
export function showFailure(message: string): void {
  showFlash(message, { tone: 'danger', durationMs: NOTICE_MS })
}

/**
 * Shows a change the user did not ask for or an action that had nothing to do, such as a
 * cleared selection, in a warning banner.
 */
export function showWarning(message: string): void {
  showFlash(message, { tone: 'warning', durationMs: NOTICE_MS })
}

/** The server's reason for `problem`; an unexpected server error points to the server log. */
export function problemReason(problem: Pick<Problem, 'code' | 'detail'>): string {
  return problem.code === 'internal_error' ? `${problem.detail} ${SERVER_LOG_HINT}` : problem.detail
}
