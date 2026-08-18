import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import WriteStatusDrawer from './WriteStatusDrawer.svelte'
import { testProviderWriteController } from '../../test/provider-write'

describe('WriteStatusDrawer', () => {
  afterEach(cleanup)

  it('shows counts-only progress and pauses without treating close as cancel', async () => {
    const controller = testProviderWriteController()
    const onclose = vi.fn()
    render(WriteStatusDrawer, { controller, onclose, onreconnect: vi.fn() })

    expect(screen.getByRole('dialog', { name: 'Monarch write status' })).not.toBeNull()
    expect(screen.getByText('2 of 5 complete')).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Pause' }))
    expect(controller.pause).toHaveBeenCalledTimes(1)
    expect(onclose).not.toHaveBeenCalled()
  })

  it('hides retry for reconcile-only attention and confirms removals explicitly', async () => {
    const controller = testProviderWriteController({
      state: {
        phase: 'confirmation',
        announcement: 'Confirm provider removals.',
        status: {
          ...testProviderWriteController().state.status!,
          phase: 'reconcile_confirmation_required',
          actions: ['confirm'],
        },
      },
      can: vi.fn((action: string) => action === 'confirm'),
    })
    render(WriteStatusDrawer, { controller, onclose: vi.fn(), onreconnect: vi.fn() })

    expect(screen.queryByRole('button', { name: 'Resume' })).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm reconciliation' }))
    expect(controller.confirm).toHaveBeenCalledTimes(1)
  })
})
