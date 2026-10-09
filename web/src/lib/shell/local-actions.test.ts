import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  confirmDelete,
  hideRow,
  localActions,
  moveCursor,
  openEditor,
  runRowAction,
  type LocalActionContext,
} from './local-actions'
import { OverlayState } from './overlay-state.svelte'
import { status } from './status.svelte'
import type { ViewController } from '../controller/view-controller.svelte'
import { testAggregateRow, testProjection } from '../../test/projection'

function setup(cursorIndex: number): { context: LocalActionContext; overlays: OverlayState } {
  const grid = document.createElement('div')
  document.body.append(grid)
  const overlays = new OverlayState({
    pushScope: () => undefined,
    focusGrid: vi.fn(),
    grid: () => grid,
  })
  const projection = testProjection({
    view: {
      mode: 'aggregate',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
      sort_fields: ['merchant', 'count', 'amount'],
    },
    detail_rows: null,
    total_rows: 2,
    window: { offset: 0, limit: 200, count: 2 },
    aggregate_rows: [testAggregateRow(), testAggregateRow({ index: 1, identity: 'agg-1' })],
  })
  const submit = vi.fn(() => Promise.resolve(true))
  const transactionInformation = vi.fn(() => Promise.resolve(undefined))
  const controller = {
    projection,
    cursorIndex,
    editing: { submit },
    transactionInformation,
  } as unknown as ViewController
  const context: LocalActionContext = {
    controller,
    overlays,
    afterTransitions: () => Promise.resolve(),
    importAmazon: vi.fn(),
    focusSearch: vi.fn(),
    openFilters: vi.fn(),
  }
  return { context, overlays }
}

describe('editor actions', () => {
  afterEach(() => document.body.replaceChildren())

  it('m and c open the editor for the cursor row', () => {
    const { context, overlays } = setup(1)
    localActions(context)['edit.category']()
    expect(overlays.current).toBe('category')
    expect(overlays.editTarget).toEqual({ identity: 'agg-1', kind: 'aggregate' })
    overlays.close()
    expect(overlays.editTarget).toBeUndefined()
    localActions(context)['edit.merchant']()
    expect(overlays.current).toBe('merchant')
    expect(overlays.editTarget).toEqual({ identity: 'agg-1', kind: 'aggregate' })
  })

  it('opens the editor for a pointed row instead of the cursor row', () => {
    const { context, overlays } = setup(1)
    openEditor(context, 'merchant', { identity: 'row-7', kind: 'transaction' })
    expect(overlays.current).toBe('merchant')
    expect(overlays.editTarget).toEqual({ identity: 'row-7', kind: 'transaction' })
  })

  it('opens no editor without a cursor row', () => {
    const { context, overlays } = setup(5)
    localActions(context)['edit.merchant']()
    expect(overlays.current).toBeUndefined()
    expect(overlays.editTarget).toBeUndefined()
  })
})

describe('row actions', () => {
  afterEach(() => document.body.replaceChildren())
  const row = { identity: 'row-7', kind: 'transaction' } as const

  it('h hides the cursor row and the sheet hides a given row through the same handler', () => {
    const { context } = setup(1)
    localActions(context)['edit.hide']()
    hideRow(context, row)
    expect(context.controller.editing.submit).toHaveBeenNthCalledWith(1, {
      action: 'transaction.toggle-hidden',
      input: {},
      target: { identity: 'agg-1', kind: 'aggregate' },
    })
    expect(context.controller.editing.submit).toHaveBeenNthCalledWith(2, {
      action: 'transaction.toggle-hidden',
      input: {},
      target: row,
    })
  })

  it('asks to confirm deleting a given transaction row, but never a grouped row', () => {
    const { context, overlays } = setup(1)
    confirmDelete(context, { identity: 'agg-1', kind: 'aggregate' })
    expect(overlays.current).toBeUndefined()
    confirmDelete(context, row)
    expect(overlays.current).toBe('delete')
    expect(overlays.deleteTarget).toEqual(row)
    expect(overlays.deleteCount).toBe(1)
  })
})

describe('runRowAction', () => {
  afterEach(() => document.body.replaceChildren())
  const row = { identity: 'row-7', kind: 'transaction' } as const

  function fromSheet() {
    const { context, overlays } = setup(1)
    overlays.open('actions', document.createElement('button'))
    return { context, overlays }
  }

  it('opens the editors and the delete confirmation for the sheet row', () => {
    for (const [action, overlay] of [
      ['edit.merchant', 'merchant'],
      ['edit.category', 'category'],
      ['edit.delete', 'delete'],
    ] as const) {
      const { context, overlays } = fromSheet()
      runRowAction(context, action, row)
      expect(overlays.current).toBe(overlay)
      expect(overlays.editTarget ?? overlays.deleteTarget).toEqual(row)
    }
  })

  it('stages hiding the sheet row and closes the sheet', () => {
    const { context, overlays } = fromSheet()
    runRowAction(context, 'edit.hide', row)
    expect(context.controller.editing.submit).toHaveBeenCalledWith({
      action: 'transaction.toggle-hidden',
      input: {},
      target: row,
    })
    expect(overlays.current).toBeUndefined()
  })

  it('asks for the sheet row’s Info', () => {
    const { context } = fromSheet()
    runRowAction(context, 'transaction.info', row)
    expect(context.controller.transactionInformation).toHaveBeenCalledWith('row-7')
  })
})

describe('moveCursor', () => {
  it('clears the status notice before the cursor moves', () => {
    status.notify('Open the group to delete its transactions.')
    const move = vi.fn(() => {
      expect(status.notice).toBe('')
      return Promise.resolve()
    })
    moveCursor(move)
    expect(move).toHaveBeenCalledTimes(1)
    expect(status.notice).toBe('')
  })
})
