import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ViewController } from '../controller/view-controller.svelte'
import { openEditor, type LocalActionContext } from '../shell/local-actions'
import { OverlayState } from '../shell/overlay-state.svelte'

const row = { identity: 'row-7', kind: 'transaction' } as const

function setup(): { context: LocalActionContext; overlays: OverlayState } {
  const overlays = new OverlayState({
    pushScope: () => undefined,
    focusGrid: vi.fn(),
    grid: () => undefined,
  })
  const context = {
    controller: { projection: undefined } as unknown as ViewController,
    overlays,
  } as LocalActionContext
  return { context, overlays }
}

const empty = { text: '', scope: 'transactions', created: undefined, groupId: '' }

describe('the editor draft', () => {
  afterEach(() => document.body.replaceChildren())

  it('starts empty for each new merchant or category editor', () => {
    const { context, overlays } = setup()
    openEditor(context, 'merchant', row)
    const first = overlays.editorDraft
    expect(first).toMatchObject(empty)
    first!.text = 'Example Merchant'
    first!.scope = 'entity'
    overlays.close()

    openEditor(context, 'category', row)
    expect(overlays.editorDraft).not.toBe(first)
    expect(overlays.editorDraft).toMatchObject(empty)
    expect(overlays.editorDraft?.opened).toBeUndefined()
  })

  it.each([
    ['Cancel or Stage in the popover', undefined],
    ['Stage from a phone sheet', 'grid'],
    ['a press outside', 'keep'],
  ] as const)('is dropped on %s', (_closing, focus) => {
    const { context, overlays } = setup()
    openEditor(context, 'merchant', row)
    overlays.editorDraft!.text = 'Example Merchant'
    overlays.close(focus)
    expect(overlays.editorDraft).toBeUndefined()
  })
})
