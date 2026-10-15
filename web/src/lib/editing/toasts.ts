import type { OperationSummary } from '../api/client'

export type ToastAction = 'mutate' | 'undo' | 'redo'

const UNDO_HINT = 'Undo u'
const REDO_HINT = 'Redo U'

const PAST_TENSE = { hide: 'Hidden', show: 'Shown', delete: 'Deleted' } as const

/**
 * Words the status line notice for a hide, show, delete, undo, or redo. Staged renames, recategorizations,
 * and taxonomy edits return `undefined`: the pending card already shows them.
 *
 * Undo and redo of a hide, show, or delete read from `kind`, which keeps the direction the edit
 * had when staged. The review label also names the direction ("Hide transactions" or "Show
 * transactions"); the notice reads `kind`.
 */
export function toastMessage(
  operation: OperationSummary | undefined,
  action: ToastAction,
): string | undefined {
  if (!operation) return undefined
  const { kind, affected, label, undoable } = operation
  const transactionEdit = kind === 'hide' || kind === 'show' || kind === 'delete'
  if (action === 'mutate') {
    if (!transactionEdit) return undefined
    return withHint(`${PAST_TENSE[kind]} ${transactionCount(affected)}`, undoable)
  }
  const edit = transactionEdit ? `${kind} · ${transactionCount(affected)}` : label
  if (action === 'undo') return `Undid ${edit} · ${REDO_HINT}`
  return withHint(`Redid ${edit}`, undoable)
}

function withHint(message: string, undoable: boolean): string {
  return undoable ? `${message} · ${UNDO_HINT}` : message
}

/** "1 transaction" or "N transactions". */
export function transactionCount(count: number): string {
  return `${count} ${count === 1 ? 'transaction' : 'transactions'}`
}
