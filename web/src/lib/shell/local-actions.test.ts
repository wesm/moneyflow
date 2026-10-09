import { afterEach, describe, expect, it, vi } from 'vitest'

import { localActions, moveCursor, openEditor, type LocalActionContext } from './local-actions'
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
  const controller = { projection, cursorIndex } as unknown as ViewController
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
