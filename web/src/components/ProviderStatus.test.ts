import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ProviderStatus from './ProviderStatus.svelte'
import type { ProviderController } from '../lib/controller/provider'

describe('ProviderStatus', () => {
  afterEach(cleanup)

  it('shows the unbound reason and disables refresh', () => {
    render(ProviderStatus, { controller: stubProvider(false) })
    const button = screen.getByRole('button', { name: 'Refresh provider data' })
    expect(button.hasAttribute('disabled')).toBe(true)
    expect(screen.getByText('Connect a provider before refreshing.')).not.toBeNull()
  })

  it('announces bounded progress and confirms suspicious removals by keyboard', async () => {
    const controller = stubProvider(true, {
      phase: 'confirmation',
      announcement: 'Confirm provider removals.',
      status: {
        version: '1',
        revision: '1',
        generation: '1',
        progress: { fetched: 20, total: 40 },
        summary: {
          imported_accounts: 0,
          imported_merchants: 0,
          imported_groups: 0,
          imported_categories: 0,
          imported_transactions: 0,
          removed_transactions: 5,
          removed_operations: 0,
          removed_targets: 0,
          retained_operations: 0,
          rebased_hide_targets: 0,
          discarded_redo_operations: 0,
        },
        capability: capability(true),
        confirmation_token: 'opaque',
      },
    })
    render(ProviderStatus, { controller })
    expect(screen.getByRole('status').textContent).toContain('20 of 40')
    const dialog = screen.getByRole('dialog', { name: 'Confirm provider refresh' })
    expect(dialog.textContent).toContain('5 posted transactions')
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    expect(controller.confirm).toHaveBeenCalledTimes(1)
  })

  it('offers in-place reconnect when the provider session expires', async () => {
    const controller = stubProvider(false, { phase: 'reconnect' })
    const onreconnect = vi.fn()
    render(ProviderStatus, { controller, onreconnect })

    await fireEvent.click(screen.getByRole('button', { name: 'Reconnect provider' }))
    expect(onreconnect).toHaveBeenCalledTimes(1)
  })
})

function stubProvider(
  available: boolean,
  state: Partial<ProviderController['state']> = {},
): ProviderController {
  return {
    state: {
      phase: 'idle',
      announcement: '',
      capability: capability(
        available,
        available ? undefined : 'Connect a provider before refreshing.',
      ),
      ...state,
    },
    sync: vi.fn(),
    poll: vi.fn(async () => undefined),
    refresh: vi.fn(async () => true),
    confirm: vi.fn(async () => true),
    dismissConfirmation: vi.fn(),
  }
}

function capability(available: boolean, reason?: string) {
  return {
    id: 'provider.refresh',
    key_display: 'r',
    description: 'Refresh provider data',
    category: 'System',
    available,
    ...(reason === undefined ? {} : { reason }),
  }
}
