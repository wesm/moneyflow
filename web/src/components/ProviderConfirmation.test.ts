import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'

import ProviderConfirmation from './ProviderConfirmation.svelte'
import { stubProvider, testProviderStatus } from '../test/provider'

function confirming() {
  const status = testProviderStatus({ confirmation_token: 'opaque' })
  status.summary.removed_transactions = 5
  return stubProvider(true, {
    phase: 'confirmation',
    announcement: 'Confirm provider removals.',
    status,
  })
}

describe('ProviderConfirmation', () => {
  afterEach(cleanup)

  it('confirms suspicious removals by keyboard', async () => {
    const controller = confirming()
    render(ProviderConfirmation, { controller })
    const dialog = screen.getByRole('dialog', { name: 'Confirm provider refresh' })
    expect(dialog.textContent).toContain('5 posted transactions')
    await fireEvent.keyDown(dialog, { key: 'Enter' })
    expect(controller.confirm).toHaveBeenCalledTimes(1)
  })

  it('dismisses with Escape', async () => {
    const controller = confirming()
    render(ProviderConfirmation, { controller })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(controller.dismissConfirmation).toHaveBeenCalled()
    expect(controller.confirm).not.toHaveBeenCalled()
  })

  it('renders nothing outside confirmation', () => {
    render(ProviderConfirmation, { controller: stubProvider(true) })
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
