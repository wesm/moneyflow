import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MerchantDialog from './MerchantDialog.svelte'
import { testEditingController } from '../../test/editing'

describe('MerchantDialog', () => {
  afterEach(cleanup)
  it('shows affected transaction context and stages a collision with one submit', async () => {
    const controller = testEditingController()
    render(MerchantDialog, {
      props: {
        controller,
        target: { kind: 'aggregate', identity: 'row-a' },
        hasSelection: false,
        onclose: vi.fn(),
      },
    })
    await vi.waitFor(() => expect(screen.getByText('5 transactions affected')).toBeTruthy())
    expect(screen.getByText('Showing 1 of 5 transactions')).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: 'Date' })).toBeTruthy()
    expect(screen.getByText('2024-01-01')).toBeTruthy()
    expect(screen.getByText(/Example Category/)).toBeTruthy()
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

  it('loads the selected transaction scope', async () => {
    const controller = testEditingController()
    render(MerchantDialog, {
      props: {
        controller,
        target: { kind: 'aggregate', identity: 'row-a' },
        hasSelection: true,
        onclose: vi.fn(),
      },
    })
    await vi.waitFor(() => expect(screen.getByText('5 transactions affected')).toBeTruthy())
    expect(controller.previewMerchant).toHaveBeenCalledWith(
      { kind: 'aggregate', identity: 'row-a' },
      'transactions',
      expect.any(AbortSignal),
    )
  })
})
