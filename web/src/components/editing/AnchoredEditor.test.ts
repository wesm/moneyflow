import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { OverlayState } from '../../lib/shell/overlay-state.svelte'
import AnchoredEditorHarness from '../../test/AnchoredEditorHarness.svelte'

function rect(left: number, top: number, width: number, height: number): DOMRect {
  return DOMRect.fromRect({ x: left, y: top, width, height })
}

/** A table row whose Merchant cell starts 200 px in. */
function row(top: number): HTMLElement {
  const element = document.createElement('div')
  element.innerHTML = '<div data-column="merchant"></div>'
  element.getBoundingClientRect = () => rect(0, top, 1000, 32)
  const merchant = element.firstElementChild as HTMLElement
  merchant.getBoundingClientRect = () => rect(200, top, 300, 32)
  document.body.append(element)
  return element
}

function setup(anchor: HTMLElement | undefined, above = false) {
  const overlays = new OverlayState({
    pushScope: () => undefined,
    focusGrid: vi.fn(),
    grid: () => undefined,
  })
  const oncancel = vi.fn()
  const ondismiss = vi.fn()
  render(AnchoredEditorHarness, { props: { anchor, above, overlays, oncancel, ondismiss } })
  const dialog = screen.getByRole('dialog', { name: 'Edit merchant' })
  return { dialog, oncancel, ondismiss }
}

describe('AnchoredEditor', () => {
  afterEach(() => {
    cleanup()
    document.body.replaceChildren()
  })

  it('is a dialog named by its title and described by its count', () => {
    const { dialog } = setup(row(100))
    expect(dialog.getAttribute('aria-describedby')).toBe('editor-count')
    expect(dialog.classList.contains('kit-popover-card')).toBe(true)
  })

  it("hangs below the anchor row, lined up with the field's cell", () => {
    const { dialog } = setup(row(100))
    expect(dialog.style.left).toBe('200px')
    expect(dialog.style.top).toBe('136px')
  })

  it('sits above the selection bar', () => {
    const bar = document.createElement('section')
    bar.getBoundingClientRect = () => rect(0, 700, 1000, 48)
    document.body.append(bar)
    const { dialog } = setup(bar, true)
    expect(Number.parseFloat(dialog.style.top)).toBeLessThan(700)
  })

  it('cancels on an Escape no control inside claimed', async () => {
    const { dialog, oncancel } = setup(row(100))
    expect(await fireEvent.keyDown(dialog, { key: 'Escape' })).toBe(false)
    expect(oncancel).toHaveBeenCalledTimes(1)
  })

  it('dismisses on a press outside, but not inside', async () => {
    const { dialog, ondismiss } = setup(row(100))
    await fireEvent.mouseDown(screen.getByRole('textbox', { name: 'Merchant' }))
    expect(ondismiss).not.toHaveBeenCalled()
    await fireEvent.mouseDown(document.body)
    expect(ondismiss).toHaveBeenCalledTimes(1)
    expect(dialog.isConnected).toBe(true)
  })
})
