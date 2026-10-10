import type { OperationSummary } from '../api/client'

export type ToastAction = 'mutate' | 'undo' | 'redo'

const UNDO_HINT = 'Undo u'
const REDO_HINT = 'Redo U'

/**
 * Words the toast for a hide, show, delete, undo, or redo. Staged renames, recategorizations,
 * and taxonomy edits return `undefined`: the pending card already shows them.
 */
export function toastMessage(
  operation: OperationSummary | undefined,
  action: ToastAction,
): string | undefined {
  if (!operation) return undefined
  if (action === 'undo') return `Undid ${operation.label} · ${REDO_HINT}`
  if (action === 'redo') return withHint(`Redid ${operation.label}`, operation.undoable)
  const count = transactions(operation.affected)
  if (operation.kind === 'hide') return withHint(`Hidden ${count}`, operation.undoable)
  if (operation.kind === 'show') return withHint(`Shown ${count}`, operation.undoable)
  if (operation.kind === 'delete') return withHint(`Deleted ${count}`, operation.undoable)
  return undefined
}

function withHint(message: string, undoable: boolean): string {
  return undoable ? `${message} · ${UNDO_HINT}` : message
}

function transactions(count: number): string {
  return `${count} ${count === 1 ? 'transaction' : 'transactions'}`
}
