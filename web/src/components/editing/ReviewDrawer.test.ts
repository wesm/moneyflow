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
    await fireEvent.click(screen.getByRole('button', { name: /Rename merchant/ }))
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
            label: 'Rename merchant',
            active: true,
            affected_count: 201,
          },
        ],
      },
    })
    render(ReviewDrawer, { editing, review, onclose: vi.fn() })
    await fireEvent.click(screen.getByRole('button', { name: /Rename merchant/ }))
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(review.loadTargets).toHaveBeenLastCalledWith('operation-a', 100)
    expect(screen.getByText('Showing 101–200 of 201')).not.toBeNull()
  })

  it('annotates zero-transaction structural operations without provider item language', () => {
    const review = testReviewController({
      state: {
        ...testReviewController().state,
        activeOperations: [
          {
            operation_id: 'operation-empty',
            type: 'merchant.label',
            label: 'Rename merchant',
            active: true,
            affected_count: 0,
          },
        ],
      },
    })
    render(ReviewDrawer, { editing: testEditingController(), review, onclose: vi.fn() })
    expect(screen.getByRole('button', { name: /affects 0 transactions/ })).not.toBeNull()
    expect(screen.queryByText(/provider item/i)).toBeNull()
  })

  it('renders the presentation label supplied for a deletion', () => {
    const review = testReviewController({
      state: {
        ...testReviewController().state,
        activeOperations: [
          {
            operation_id: 'delete-one',
            type: 'transaction.delete',
            label: 'Delete transaction',
            active: true,
            affected_count: 1,
          },
        ],
      },
    })
    render(ReviewDrawer, { editing: testEditingController(), review, onclose: vi.fn() })

    expect(screen.getByRole('dialog', { name: 'Review pending changes' }).textContent).toContain(
      'Delete transaction',
    )
  })

  it('commits with Enter from the drawer and opens provider write status without extra ceremony', async () => {
    const editing = testEditingController({
      state: {
        ...testEditingController().state,
        providerWrite: {
          version: '1',
          revision: '2',
          generation: '1',
          batch_version: '1',
          phase: 'writing',
          total: 1,
          completed: 0,
          failed: 0,
          remaining: 1,
          overrides: 0,
          actions: ['pause'],
        },
      },
    })
    const onwrite = vi.fn()
    render(ReviewDrawer, {
      editing,
      review: testReviewController(),
      onclose: vi.fn(),
      onwrite,
    })

    screen.getByRole('dialog', { name: 'Review pending changes' }).focus()
    await fireEvent.keyDown(window, { key: 'Enter' })

    expect(editing.commit).toHaveBeenCalledTimes(1)
    expect(onwrite).toHaveBeenCalledTimes(1)
  })
})
