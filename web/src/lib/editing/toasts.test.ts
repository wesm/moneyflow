import { describe, expect, it } from 'vitest'

import { toastMessage } from './toasts'
import type { OperationSummary } from '../api/client'

function operation(overrides: Partial<OperationSummary>): OperationSummary {
  return { kind: 'hide', label: 'Hide transactions', affected: 3, undoable: true, ...overrides }
}

describe('toastMessage', () => {
  it('reports a staged hide with the undo hint', () => {
    expect(toastMessage(operation({ kind: 'hide', affected: 3 }), 'mutate')).toBe(
      'Hidden 3 transactions · Undo u',
    )
  })

  it('uses the singular form for one transaction', () => {
    expect(toastMessage(operation({ kind: 'hide', affected: 1 }), 'mutate')).toBe(
      'Hidden 1 transaction · Undo u',
    )
    expect(toastMessage(operation({ kind: 'delete', affected: 1 }), 'mutate')).toBe(
      'Deleted 1 transaction · Undo u',
    )
  })

  it('reports a staged show and a staged delete', () => {
    expect(toastMessage(operation({ kind: 'show', affected: 2 }), 'mutate')).toBe(
      'Shown 2 transactions · Undo u',
    )
    expect(toastMessage(operation({ kind: 'delete', affected: 4 }), 'mutate')).toBe(
      'Deleted 4 transactions · Undo u',
    )
  })

  it('leaves out the undo hint when the edit cannot be undone', () => {
    expect(toastMessage(operation({ kind: 'show', affected: 2, undoable: false }), 'mutate')).toBe(
      'Shown 2 transactions',
    )
  })

  it('stays silent for staged edits the pending card already shows', () => {
    for (const kind of ['rename', 'recategorize', 'manage_category', 'manage_group'] as const) {
      expect(toastMessage(operation({ kind }), 'mutate')).toBeUndefined()
    }
  })

  it('names the undone and redone operation with the server label', () => {
    const label = 'Rename Example Merchant'
    expect(toastMessage(operation({ kind: 'rename', label }), 'undo')).toBe(
      'Undid Rename Example Merchant · Redo U',
    )
    expect(toastMessage(operation({ kind: 'rename', label }), 'redo')).toBe(
      'Redid Rename Example Merchant · Undo u',
    )
  })

  it('has no message without an operation', () => {
    expect(toastMessage(undefined, 'mutate')).toBeUndefined()
    expect(toastMessage(undefined, 'undo')).toBeUndefined()
  })
})
