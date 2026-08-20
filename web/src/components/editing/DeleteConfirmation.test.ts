import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import DeleteConfirmation from './DeleteConfirmation.svelte'

describe('DeleteConfirmation', () => {
  afterEach(cleanup)

  it('shows only the bounded count and requires explicit confirmation', async () => {
    const confirm = vi.fn()
    const cancel = vi.fn()
    render(DeleteConfirmation, { count: 2, onconfirm: confirm, oncancel: cancel })

    const dialog = screen.getByRole('dialog', { name: 'Confirm deletion' })
    expect(dialog.textContent).toContain('Delete 2 transactions?')
    expect(dialog.textContent).toContain('nothing reaches the provider until review and commit')
    expect(dialog.textContent).not.toContain('Example Merchant')
    expect(confirm).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: 'Stage deletion' }))
    expect(confirm).toHaveBeenCalledTimes(1)
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(cancel).toHaveBeenCalledTimes(1)
  })

  it('does not submit twice when Enter activates the focused button', async () => {
    const confirm = vi.fn()
    render(DeleteConfirmation, { count: 1, onconfirm: confirm, oncancel: vi.fn() })

    const button = screen.getByRole('button', { name: 'Stage deletion' })
    button.focus()
    await fireEvent.keyDown(button, { key: 'Enter' })
    await fireEvent.click(button)

    expect(confirm).toHaveBeenCalledTimes(1)
  })

  it('does not close through modal chrome while submitting', async () => {
    const cancel = vi.fn()
    render(DeleteConfirmation, {
      count: 1,
      onconfirm: vi.fn(),
      oncancel: cancel,
      submitting: true,
    })

    await fireEvent.click(screen.getByRole('button', { name: 'Cancel deletion' }))

    expect(cancel).not.toHaveBeenCalled()
  })
})
