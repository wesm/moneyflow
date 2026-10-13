import { dismissFlash, getFlashes } from '@kenn-io/kit-ui'
import { tick } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  confirmDelete,
  confirmDirectDelete,
  hideRow,
  localActions,
  moveCursor,
  openEditor,
  openEditorWhenSettled,
  runRowAction,
  type LocalActionContext,
} from './local-actions'
import { OverlayState } from './overlay-state.svelte'
import { status } from './status.svelte'
import { setPageRows } from '../table/cursor-keys'
import type { ViewController } from '../controller/view-controller.svelte'
import { testCapabilities } from '../../test/capabilities'
import { testAggregateRow, testProjection } from '../../test/projection'

/** Queues a view transition that stays in flight until the returned function is called. */
function holdTransitions(context: LocalActionContext): () => void {
  let settle!: () => void
  const held = new Promise<void>((resolve) => (settle = resolve))
  void context.queue(() => held)
  return settle
}

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
      drill_depth: 0,
      root: { mode: 'aggregate', grouping: 'merchant', time_granularity: 'year' },
    },
    detail_rows: null,
    selection_count: 0,
    capabilities: testCapabilities([
      'transaction.edit-merchant',
      'transaction.edit-category',
      'transaction.toggle-hidden',
      'transaction.delete',
    ]),
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
  let tail: Promise<unknown> = Promise.resolve()
  const context: LocalActionContext = {
    controller,
    overlays,
    queue: (work) => (tail = tail.then(work, work)),
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
    const settle = holdTransitions(context)
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

  it('opens a pointed row’s editor after queued transitions, as m and c do', async () => {
    const { context, overlays } = setup(1)
    const settle = holdTransitions(context)
    openEditorWhenSettled(context, 'merchant', { identity: 'row-7', kind: 'transaction' })
    await tick()
    expect(overlays.current).toBeUndefined()

    settle()
    await vi.waitFor(() => expect(overlays.current).toBe('merchant'))
    expect(overlays.editTarget).toEqual({ identity: 'row-7', kind: 'transaction' })
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

  it('h holds later view transitions until the hide is staged', async () => {
    const { context } = setup(1)
    let staged!: (accepted: boolean) => void
    vi.mocked(context.controller.editing.submit).mockImplementationOnce(
      () => new Promise<boolean>((resolve) => (staged = resolve)),
    )
    localActions(context)['edit.hide']()
    const marker = vi.fn()
    const after = context.queue(marker)
    await vi.waitFor(() => expect(context.controller.editing.submit).toHaveBeenCalledTimes(1))
    await tick()
    expect(marker).not.toHaveBeenCalled()

    staged(true)
    await after
    expect(marker).toHaveBeenCalledTimes(1)
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
    const settle = holdTransitions(context)
    runRowAction(context, 'edit.hide', row)
    await tick()
    expect(context.controller.editing.submit).not.toHaveBeenCalled()
    settle()
    await vi.waitFor(() => expect(context.controller.editing.submit).toHaveBeenCalledTimes(1))
  })

  it('leaves the edit to the selection bar when a queued toggle selects rows', async () => {
    for (const action of ['edit.merchant', 'edit.hide'] as const) {
      const { context, overlays } = fromSheet()
      const settle = holdTransitions(context)
      runRowAction(context, action, row)
      ;(context.controller.projection as { selection_count: number }).selection_count = 1
      settle()
      await vi.waitFor(() =>
        expect(status.notice).toBe('Clear the selection to edit one transaction'),
      )
      expect(overlays.current).not.toBe('merchant')
      expect(context.controller.editing.submit).not.toHaveBeenCalled()
      status.clear()
    }
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

describe('failures with no dialog to show them', () => {
  afterEach(() => {
    document.body.replaceChildren()
    dismissFlash()
  })
  const row = { identity: 'row-7', kind: 'transaction' } as const
  const flashes = () => getFlashes().map(({ message, tone }) => ({ message, tone }))

  /** A controller whose edits resolve `false`, leaving `phase` and `announcement`. */
  function refusing(phase: string, announcement: string) {
    const { context } = setup(1)
    const refuse = vi.fn(() => Promise.resolve(false))
    Object.assign(context.controller, {
      editing: { submit: refuse, undo: refuse, redo: refuse, state: { phase, announcement } },
    })
    return context
  }

  it('shows why a hide from h, the sheet, or the selection bar did not apply', async () => {
    const context = refusing('failed', 'The profile is busy. Try the action again when ready.')
    await hideRow(context, row)
    expect(flashes()).toEqual([
      { message: 'The profile is busy. Try the action again when ready.', tone: 'danger' },
    ])
  })

  it('shows why an undo or redo did not apply', async () => {
    const context = refusing('conflict', 'The profile changed in another session.')
    localActions(context)['edit.undo']()
    localActions(context)['edit.redo']()
    await vi.waitFor(() => expect(flashes()).toHaveLength(2))
    expect(flashes()[0]).toEqual({
      message: 'The profile changed in another session.',
      tone: 'danger',
    })
  })

  it('warns that a refreshed selection needs the action again', async () => {
    const context = refusing('idle', 'Selection refreshed. Invoke the action again.')
    await hideRow(context, row)
    expect(flashes()).toEqual([
      { message: 'Selection refreshed. Invoke the action again.', tone: 'warning' },
    ])
  })

  function exporting(phase: string, announcement: string) {
    const { context, overlays } = setup(1)
    const open = vi.fn(() => Promise.resolve(false))
    Object.assign(context.controller, { export: { open, state: { phase, announcement } } })
    return { context, overlays }
  }

  it('shows why the export preview failed instead of opening nothing', async () => {
    const message = 'The export preview could not be loaded. The profile is busy. Try again.'
    const { context, overlays } = exporting('failed', message)
    localActions(context)['transactions.export']()
    await vi.waitFor(() => expect(flashes()).toEqual([{ message, tone: 'danger' }]))
    expect(overlays.current).toBeUndefined()
  })

  it('says there is no data to export', async () => {
    const { context } = exporting('idle', 'No data to export.')
    localActions(context)['transactions.export']()
    await vi.waitFor(() =>
      expect(flashes()).toEqual([{ message: 'No data to export.', tone: 'warning' }]),
    )
  })
})

describe('moveCursor', () => {
  afterEach(() => document.body.replaceChildren())

  it('clears the status notice at once and moves after pending view changes', async () => {
    const { context } = setup(0)
    const settle = holdTransitions(context)
    status.notify('Open the group to delete its transactions.')
    const move = vi.fn(() => Promise.resolve())
    const moved = moveCursor(context, move)
    expect(status.notice).toBe('')
    await tick()
    expect(move).not.toHaveBeenCalled()
    settle()
    await moved
    expect(move).toHaveBeenCalledTimes(1)
  })
})

describe('paging keys outside the table', () => {
  afterEach(() => {
    setPageRows(1)
    document.body.replaceChildren()
  })

  function paged(cursorIndex: number) {
    const moveCursorTo = vi.fn(() => Promise.resolve())
    const controller = {
      projection: testProjection({ total_rows: 500 }),
      cursorIndex,
      moveCursorTo,
    } as unknown as ViewController
    const context = { ...setup(0).context, controller }
    setPageRows(18)
    return { context, controller, moveCursorTo, actions: localActions(context) }
  }

  it('move the cursor by the page the table last showed, or to the last row', async () => {
    const { actions, moveCursorTo } = paged(40)
    actions['cursor.page-down']()
    await vi.waitFor(() => expect(moveCursorTo).toHaveBeenLastCalledWith(58))
    actions['cursor.page-up']()
    await vi.waitFor(() => expect(moveCursorTo).toHaveBeenLastCalledWith(22))
    actions['cursor.end']()
    await vi.waitFor(() => expect(moveCursorTo).toHaveBeenLastCalledWith(499))
  })

  it('move from the cursor a pending view change leaves', async () => {
    const { actions, context, controller, moveCursorTo } = paged(40)
    const settle = holdTransitions(context)
    actions['cursor.page-down']()
    await tick()
    expect(moveCursorTo).not.toHaveBeenCalled()
    Object.assign(controller, { cursorIndex: 10 })
    settle()

    await vi.waitFor(() => expect(moveCursorTo).toHaveBeenCalledWith(28))
  })
})
