import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CategoryDialog from './CategoryDialog.svelte'
import { testEditingController } from '../../test/editing'

describe('CategoryDialog', () => {
  afterEach(cleanup)
  it('submits an existing stable category for the focused target', async () => {
    const controller = testEditingController()
    render(CategoryDialog, {
      props: {
        controller,
        target: { kind: 'detail', identity: 'transaction-a' },
        onclose: vi.fn(),
      },
    })
    await vi.waitFor(() =>
      expect(screen.getByRole('combobox', { name: /Category:/ })).not.toBeNull(),
    )
    await fireEvent.click(screen.getByRole('button', { name: 'Save pending change' }))
    expect(controller.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'transaction.edit-category',
        input: expect.objectContaining({ scope: 'transactions' }),
      }),
    )
  })
})
