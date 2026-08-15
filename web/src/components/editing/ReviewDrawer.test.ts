import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ReviewDrawer from './ReviewDrawer.svelte'
import { testEditingController, testReviewController } from '../../test/editing'

describe('ReviewDrawer', () => {
  afterEach(cleanup)
  it('separates redo history, expands targets on demand, and commits the captured revision', async () => {
    const editing = testEditingController()
    const review = testReviewController()
    render(ReviewDrawer, { editing, review, onclose: vi.fn() })
    expect(screen.getByRole('heading', { name: 'Active operations' })).not.toBeNull()
    expect(screen.getByRole('heading', { name: 'Inactive redo operations' })).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: /merchant.label/ }))
    expect(review.loadTargets).toHaveBeenCalledWith('operation-a', 0)
    await fireEvent.click(screen.getByRole('button', { name: 'Commit reviewed changes' }))
    expect(editing.commit).toHaveBeenCalledWith(1n)
  })

  it('loads affected transactions in bounded pages', async () => {
    const editing = testEditingController()
    const review = testReviewController({
      state: {
        ...testReviewController().state,
        activeOperations: [
          {
            operation_id: 'operation-a',
            type: 'merchant.label',
            active: true,
            affected_count: 201,
          },
        ],
      },
    })
    render(ReviewDrawer, { editing, review, onclose: vi.fn() })
    await fireEvent.click(screen.getByRole('button', { name: /merchant.label/ }))
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(review.loadTargets).toHaveBeenLastCalledWith('operation-a', 100)
    expect(screen.getByText('Showing 101–200 of 201')).not.toBeNull()
  })
})
