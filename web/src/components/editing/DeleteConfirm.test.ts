import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import DeleteConfirm from './DeleteConfirm.svelte'
import { testEditingController } from '../../test/editing'
import { OverlayState, type EditorAnchor } from '../../lib/shell/overlay-state.svelte'

/** A table row whose Merchant cell starts 200 px in, 100 px from the top. */
function row(): HTMLElement {
  const element = document.createElement('div')
  element.innerHTML = '<div data-column="merchant"></div>'
  element.getBoundingClientRect = () => DOMRect.fromRect({ x: 0, y: 100, width: 1000, height: 32 })
  const merchant = element.firstElementChild as HTMLElement
  merchant.getBoundingClientRect = () =>
    DOMRect.fromRect({ x: 200, y: 100, width: 300, height: 32 })
  document.body.append(element)
  return element
}

interface Setup {
  count?: number
  compact?: boolean
  staged?: () => Promise<boolean>
  /** The editing controller's announcement after the request. */
  announcement?: string
}

function setup(options: Setup = {}) {
  const overlays = new OverlayState({
    pushScope: () => undefined,
    focusGrid: vi.fn(),
    grid: () => undefined,
  })
  const anchor: EditorAnchor = { element: row(), above: false }
  const editing = testEditingController()
  editing.state.announcement = options.announcement ?? ''
  const onconfirm = vi.fn(options.staged ?? (() => Promise.resolve(true)))
  const oncancel = vi.fn()
  const ondismiss = vi.fn()
  render(DeleteConfirm, {
    props: {
      count: options.count ?? 2,
      compact: options.compact ?? false,
      anchor,
      overlays,
      editing,
      onconfirm,
      oncancel,
      ondismiss,
    },
  })
  const dialog = screen.getByRole('alertdialog', { name: 'Confirm deletion' })
  return { dialog, onconfirm, oncancel, ondismiss }
}

/** A promise the test settles, standing in for a staging request in flight. */
function pending() {
  let settle: (accepted: boolean) => void = () => undefined
  const promise = new Promise<boolean>((resolve) => (settle = resolve))
  return { promise, settle }
}

describe('DeleteConfirm', () => {
  afterEach(() => {
    cleanup()
    document.body.replaceChildren()
  })

  it('asks about the count only, with the Enter and Esc hints', () => {
    const { dialog } = setup({ count: 2 })
    expect(dialog.textContent).toContain('Delete 2 transactions?')
    const described = document.getElementById(dialog.getAttribute('aria-describedby') ?? '')
    expect(described?.textContent).toBe('Delete 2 transactions?')
    const hints = Array.from(dialog.querySelectorAll('kbd'), (key) =>
      key.getAttribute('aria-label'),
    )
    expect(hints).toEqual(['Enter', 'Esc'])
    expect(screen.getByRole('button', { name: 'Delete' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeTruthy()
  })

  it('uses the singular for one transaction', () => {
    const { dialog } = setup({ count: 1 })
    expect(dialog.textContent).toContain('Delete 1 transaction?')
  })

  it('hangs below the anchor row as a popover on desktop', () => {
    const { dialog } = setup()
    expect(dialog.classList.contains('kit-popover-card')).toBe(true)
    expect(dialog.style.left).toBe('200px')
    expect(dialog.closest('.mf-sheet')).toBeNull()
  })

  it('is a bottom sheet on phones', () => {
    const { dialog } = setup({ compact: true })
    expect(dialog.closest('.mf-sheet')).not.toBeNull()
    expect(dialog.getAttribute('aria-modal')).toBe('true')
  })

  it('moves focus into the confirmation', () => {
    const { dialog } = setup()
    expect(dialog.contains(document.activeElement)).toBe(true)
  })

  it('stages on Enter and cancels on Esc', async () => {
    const { dialog, onconfirm, oncancel } = setup()
    expect(await fireEvent.keyDown(dialog, { key: 'Enter' })).toBe(false)
    expect(onconfirm).toHaveBeenCalledTimes(1)
    expect(await fireEvent.keyDown(dialog, { key: 'Escape' })).toBe(false)
    expect(oncancel).toHaveBeenCalledTimes(1)
  })

  it('stages once when Enter repeats or comes again while staging runs', async () => {
    const request = pending()
    const { dialog, onconfirm } = setup({ staged: () => request.promise })
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    await fireEvent.keyDown(dialog, { key: 'Enter', repeat: true })
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onconfirm).toHaveBeenCalledTimes(1)
    request.settle(true)
    await request.promise
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    expect(onconfirm).toHaveBeenCalledTimes(1)
  })

  it('lets a new Enter, not a repeat, try again after the server refuses', async () => {
    const { dialog, onconfirm } = setup({ staged: () => Promise.resolve(false) })
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    await vi.waitFor(() => expect(onconfirm).toHaveBeenCalledTimes(1))
    await Promise.resolve()
    await fireEvent.keyDown(dialog, { key: 'Enter', repeat: true })
    expect(onconfirm).toHaveBeenCalledTimes(1)
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    expect(onconfirm).toHaveBeenCalledTimes(2)
  })

  it('shows why the server refused the delete and stays open', async () => {
    const announcement =
      'The profile changed in another session. Review the current data and try again.'
    const { dialog } = setup({ staged: () => Promise.resolve(false), announcement })
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    expect((await screen.findByRole('alert')).textContent).toBe(announcement)
    expect(dialog.isConnected).toBe(true)
  })

  it('reports a request that throws, stays open, and lets a new Enter try again', async () => {
    const staged = vi
      .fn<() => Promise<boolean>>()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(true)
    const { dialog, onconfirm } = setup({ staged })
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    expect((await screen.findByRole('alert')).textContent).toBe('The profile request failed.')
    expect(dialog.isConnected).toBe(true)
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    await vi.waitFor(() => expect(onconfirm).toHaveBeenCalledTimes(2))
    await vi.waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  })

  it('leaves Enter on Cancel, and modified Enter, to the browser', async () => {
    const { onconfirm } = setup()
    const cancel = screen.getByRole('button', { name: 'Cancel' })
    expect(await fireEvent.keyDown(cancel, { key: 'Enter' })).toBe(true)
    await fireEvent.keyDown(cancel, { key: 'Enter', ctrlKey: true })
    expect(onconfirm).not.toHaveBeenCalled()
  })

  it('confirms and cancels from its buttons', async () => {
    const { onconfirm, oncancel } = setup()
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onconfirm).toHaveBeenCalledTimes(1)
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(oncancel).toHaveBeenCalledTimes(1)
  })

  it('dismisses on a press outside', async () => {
    const { ondismiss } = setup()
    await fireEvent.mouseDown(document.body)
    expect(ondismiss).toHaveBeenCalledTimes(1)
  })
})
