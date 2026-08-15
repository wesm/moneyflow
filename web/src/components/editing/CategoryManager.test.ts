import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CategoryManager from './CategoryManager.svelte'
import { testEditingController } from '../../test/editing'

describe('CategoryManager', () => {
  afterEach(cleanup)
  it('creates a category with an explicit group through pending taxonomy intent', async () => {
    const controller = testEditingController()
    render(CategoryManager, { controller, onclose: vi.fn() })
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'New Category' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save pending operation' }))
    expect(controller.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'category.manage',
        input: expect.objectContaining({
          taxonomy: 'create',
          label: 'New Category',
          group_id: 'group-a',
        }),
      }),
    )
  })

  it('filters the bounded catalog and keeps protected sentinels unavailable', async () => {
    render(CategoryManager, { controller: testEditingController(), onclose: vi.fn() })
    await fireEvent.change(screen.getByLabelText('Operation'), { target: { value: 'rename' } })
    await vi.waitFor(() => expect(screen.getByLabelText('Filter categories')).not.toBeNull())
    const sentinel = screen.getByRole('option', { name: 'Uncategorized (protected)' })
    expect((sentinel as HTMLOptionElement).disabled).toBe(true)
    await fireEvent.input(screen.getByLabelText('Filter categories'), {
      target: { value: 'Example' },
    })
    expect(screen.queryByRole('option', { name: 'Uncategorized (protected)' })).toBeNull()
    expect(screen.getByRole('option', { name: 'Example Category' })).not.toBeNull()
  })
})
