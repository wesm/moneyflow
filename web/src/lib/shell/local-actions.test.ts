import { tick } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  confirmDelete,
  confirmDirectDelete,
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
    focusGrid: vi.fn(),
    importAmazon: vi.fn(),
    focusSearch: vi.fn(),
    openFilters: vi.fn(),
  }
  return { context, overlays }
}

describe('editor actions', () => {
  afterEach(() => document.body.replaceChildren())

  it('m and c open the editor for the cursor row', async () => {
    const { context, overlays } = setup(1)
    localActions(context)['edit.category']()
    await vi.waitFor(() => expect(overlays.current).toBe('category'))
    expect(overlays.editTarget).toEqual({ identity: 'agg-1', kind: 'aggregate' })
    overlays.close()
    expect(overlays.editTarget).toBeUndefined()
    localActions(context)['edit.merchant']()
    await vi.waitFor(() => expect(overlays.current).toBe('merchant'))
    expect(overlays.editTarget).toEqual({ identity: 'agg-1', kind: 'aggregate' })
  })

  it('waits for queued transitions before an edit reads the cursor row', async () => {
    const { context, overlays } = setup(0)
    let settle = () => {}
    const transitions = new Promise<void>((resolve) => (settle = resolve))
    context.afterTransitions = () => transitions
    const actions = localActions(context)
    actions['edit.hide']()
    actions['edit.merchant']()
    await tick()
    expect(context.controller.editing.submit).not.toHaveBeenCalled()
    expect(overlays.current).toBeUndefined()

    // The queued transition moved the cursor, as a toggle's new window can.
    ;(context.controller as { cursorIndex: number }).cursorIndex = 1
    settle()
    const target = { identity: 'agg-1', kind: 'aggregate' }
    await vi.waitFor(() => expect(overlays.editTarget).toEqual(target))
    expect(context.controller.editing.submit).toHaveBeenCalledWith({
      action: 'transaction.toggle-hidden',
      input: {},
      target,
    })
  })

  it('opens the editor for a pointed row instead of the cursor row', () => {
    const { context, overlays } = setup(1)
    openEditor(context, 'merchant', { identity: 'row-7', kind: 'transaction' })
    expect(overlays.current).toBe('merchant')
    expect(overlays.editTarget).toEqual({ identity: 'row-7', kind: 'transaction' })
  })

  it('hangs the editor below the edited row, or above the selection bar while rows are selected', () => {
    const { context, overlays } = setup(1)
    const row = document.createElement('div')
    row.id = 'moneyflow-row-row-7'
    const bar = document.createElement('section')
    bar.className = 'mf-selection-bar'
    document.body.append(row, bar)
    openEditor(context, 'merchant', { identity: 'row-7', kind: 'transaction' })
    expect(overlays.anchor).toEqual({ element: row, above: false })
    overlays.close('keep')

    context.controller.projection!.selection_count = 2
    openEditor(context, 'category', { identity: 'row-7', kind: 'transaction' })
    expect(overlays.anchor).toEqual({ element: bar, above: true })
  })

  it('hangs the editor below the row while the selection bar has not rendered', () => {
    const { context, overlays } = setup(1)
    const row = document.createElement('div')
    row.id = 'moneyflow-row-row-7'
    document.body.append(row)
    context.controller.projection!.selection_count = 2
    openEditor(context, 'category', { identity: 'row-7', kind: 'transaction' })
    expect(overlays.anchor).toEqual({ element: row, above: false })
  })

  it('opens no editor without a cursor row', async () => {
    const { context, overlays } = setup(5)
    localActions(context)['edit.merchant']()
    await tick()
    await tick()
    expect(overlays.current).toBeUndefined()
    expect(overlays.editTarget).toBeUndefined()
  })
})

describe('row actions', () => {
  afterEach(() => document.body.replaceChildren())
  const row = { identity: 'row-7', kind: 'transaction' } as const

  it('h hides the cursor row and the sheet hides a given row with the same handler', async () => {
    const { context } = setup(1)
    localActions(context)['edit.hide']()
    await vi.waitFor(() => expect(context.controller.editing.submit).toHaveBeenCalledTimes(1))
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

  it('hangs the delete confirmation below the row, or above the selection bar', () => {
    const { context, overlays } = setup(1)
    const element = document.createElement('div')
    element.id = 'moneyflow-row-row-7'
    const bar = document.createElement('section')
    bar.className = 'mf-selection-bar'
    document.body.append(element, bar)
    confirmDelete(context, row)
    expect(overlays.anchor).toEqual({ element, above: false })
    overlays.close('keep')

    context.controller.projection!.selection_count = 2
    confirmDelete(context, row)
    expect(overlays.anchor).toEqual({ element: bar, above: true })
    expect(overlays.deleteCount).toBe(2)
  })

  it('stages the confirmed delete and closes only once the server accepts it', async () => {
    const { context, overlays } = setup(1)
    const submit = vi.mocked(context.controller.editing.submit)
    submit.mockResolvedValueOnce(false)
    confirmDelete(context, row)
    expect(await confirmDirectDelete(context)).toBe(false)
    expect(overlays.current).toBe('delete')
    expect(await confirmDirectDelete(context)).toBe(true)
    expect(submit).toHaveBeenLastCalledWith({
      action: 'transaction.delete',
      input: {},
      target: row,
    })
    expect(overlays.current).toBeUndefined()
  })

  it('leaves an editor opened after the confirmation was dismissed mid-delete', async () => {
    const { context, overlays } = setup(1)
    let settle: (accepted: boolean) => void = () => undefined
    vi.mocked(context.controller.editing.submit).mockReturnValueOnce(
      new Promise<boolean>((resolve) => (settle = resolve)),
    )
    confirmDelete(context, row)
    const deleting = confirmDirectDelete(context)
    overlays.close()
    openEditor(context, 'merchant', row)
    settle(true)
    expect(await deleting).toBe(true)
    expect(overlays.current).toBe('merchant')
  })
})

describe('w', () => {
  afterEach(() => document.body.replaceChildren())

  it('opens write status while a provider write has a phase, and review otherwise', () => {
    for (const [phase, expected] of [
      ['writing', 'write'],
      ['paused', 'write'],
      [undefined, 'review'],
    ] as const) {
      const { context, overlays } = setup(0)
      const status = phase === undefined ? undefined : { phase }
      Object.assign(context.controller, { providerWrite: { state: { status } } })
      localActions(context)['edit.review']()
      expect(overlays.current, String(phase)).toBe(expected)
    }
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

  it('opens the editors and the delete confirmation for the sheet row', async () => {
    for (const [action, overlay] of [
      ['edit.merchant', 'merchant'],
      ['edit.category', 'category'],
      ['edit.delete', 'delete'],
    ] as const) {
      const { context, overlays } = fromSheet()
      runRowAction(context, action, row)
      await vi.waitFor(() => expect(overlays.current).toBe(overlay))
      expect(overlays.editTarget ?? overlays.deleteTarget).toEqual(row)
    }
  })

  it('waits for queued transitions before an edit reads the projection', async () => {
    const { context } = fromSheet()
    let settle = () => {}
    const transitions = new Promise<void>((resolve) => (settle = resolve))
    context.afterTransitions = () => transitions
    runRowAction(context, 'edit.hide', row)
    await tick()
    expect(context.controller.editing.submit).not.toHaveBeenCalled()
    settle()
    await vi.waitFor(() => expect(context.controller.editing.submit).toHaveBeenCalledTimes(1))
  })

  it('stages hiding the sheet row and closes the sheet', async () => {
    const { context, overlays } = fromSheet()
    runRowAction(context, 'edit.hide', row)
    await vi.waitFor(() => expect(context.controller.editing.submit).toHaveBeenCalledTimes(1))
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
