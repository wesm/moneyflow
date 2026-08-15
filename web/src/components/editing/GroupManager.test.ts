import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import GroupManager from './GroupManager.svelte'
import { testEditingController } from '../../test/editing'

describe('GroupManager', () => {
  afterEach(cleanup)
  it('creates a group as one pending taxonomy operation', async () => {
    const controller = testEditingController()
    render(GroupManager, { controller, onclose: vi.fn() })
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'New Group' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save pending operation' }))
    expect(controller.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'category-group.manage',
        input: expect.objectContaining({ taxonomy: 'create', label: 'New Group' }),
      }),
    )
  })
})
