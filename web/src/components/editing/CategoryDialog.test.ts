import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CategoryDialog from './CategoryDialog.svelte'
import { testCatalog, testEditingController } from '../../test/editing'

describe('CategoryDialog', () => {
  afterEach(cleanup)
  it('submits an existing stable category for the focused target', async () => {
    const controller = testEditingController()
    render(CategoryDialog, {
      props: {
        controller,
        target: { kind: 'transaction', identity: 'transaction-a' },
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

  it('submits the visible category after filtering hides the previous destination', async () => {
    const controller = testEditingController({
      catalog: vi.fn(async () => ({
        ...testCatalog,
        categories: [
          ...(testCatalog.categories ?? []),
          { id: 'category-b', label: 'Second Category', parent_id: 'group-a', protected: false },
        ],
      })),
    })
    render(CategoryDialog, {
      props: {
        controller,
        target: { kind: 'transaction', identity: 'transaction-a' },
        onclose: vi.fn(),
      },
    })
    const filter = await screen.findByRole('searchbox', { name: 'Filter categories' })
    await fireEvent.input(filter, { target: { value: 'Second' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save pending change' }))
    expect(controller.submit).toHaveBeenCalledWith(
      expect.objectContaining({ input: expect.objectContaining({ destination_id: 'category-b' }) }),
    )
  })
})
