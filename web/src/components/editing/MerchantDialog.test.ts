import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MerchantDialog from './MerchantDialog.svelte'
import { testEditingController } from '../../test/editing'

describe('MerchantDialog', () => {
  afterEach(cleanup)
  it('confirms collisions and submits renderer intent without resolving targets', async () => {
    const controller = testEditingController()
    render(MerchantDialog, {
      props: {
        controller,
        target: { kind: 'aggregate', identity: 'row-a' },
        hasSelection: false,
        onclose: vi.fn(),
      },
    })
    await fireEvent.input(screen.getByLabelText('Merchant name'), {
      target: { value: 'Example Merchant' },
    })
    await vi.waitFor(() => expect(screen.getByRole('status').textContent).toContain('merge'))
    await fireEvent.click(screen.getByRole('button', { name: 'Save pending change' }))
    expect(controller.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'transaction.edit-merchant',
        target: { kind: 'aggregate', identity: 'row-a' },
        input: expect.objectContaining({ scope: 'entity', destination_id: 'merchant-a' }),
      }),
    )
  })
})
