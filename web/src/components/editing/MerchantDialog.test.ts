import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MerchantDialog from './MerchantDialog.svelte'
import { testCatalog, testEditingController } from '../../test/editing'

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
    expect(
      screen.getByRole('option', { name: 'Example Merchant' }).getAttribute('aria-selected'),
    ).toBe('true')
    await fireEvent.click(screen.getByRole('button', { name: 'Save pending change' }))
    expect(controller.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'transaction.edit-merchant',
        target: { kind: 'aggregate', identity: 'row-a' },
        input: expect.objectContaining({ scope: 'transactions', destination_id: 'merchant-a' }),
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

  it('stages the highlighted partial match using its full name and identity', async () => {
    const controller = testEditingController()
    render(MerchantDialog, {
      props: {
        controller,
        target: { kind: 'aggregate', identity: 'row-a' },
        hasSelection: false,
        onclose: vi.fn(),
      },
    })
    await screen.findByText('5 transactions affected')
    await fireEvent.input(screen.getByLabelText('Merchant name'), {
      target: { value: 'exam' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Save pending change' }))
    expect(controller.submit).toHaveBeenCalledWith({
      action: 'transaction.edit-merchant',
      target: { kind: 'aggregate', identity: 'row-a' },
      input: { scope: 'transactions', destination_id: 'merchant-a', label: 'Example Merchant' },
    })
  })

  it('resets an explicit Create choice when the query changes', async () => {
    const controller = testEditingController()
    render(MerchantDialog, {
      props: {
        controller,
        target: { kind: 'aggregate', identity: 'row-a' },
        hasSelection: false,
        onclose: vi.fn(),
      },
    })
    await screen.findByText('5 transactions affected')
    const input = screen.getByLabelText('Merchant name')
    await fireEvent.input(input, { target: { value: 'Exam' } })
    await fireEvent.click(screen.getByRole('option', { name: 'Create “Exam”' }))
    await fireEvent.input(input, { target: { value: 'example' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save pending change' }))
    expect(controller.submit).toHaveBeenLastCalledWith(
      expect.objectContaining({
        input: { scope: 'transactions', label: 'Example Merchant', destination_id: 'merchant-a' },
      }),
    )
  })

  it('prefers an exact name over longer matches and offers no duplicate creation', async () => {
    const controller = testEditingController({
      catalog: vi.fn(async () => ({
        ...testCatalog,
        merchants: [
          { id: 'merchant-long', label: 'Example Merchant Plus', protected: false },
          ...(testCatalog.merchants ?? []),
        ],
      })),
    })
    render(MerchantDialog, {
      props: {
        controller,
        target: { kind: 'aggregate', identity: 'row-a' },
        hasSelection: false,
        onclose: vi.fn(),
      },
    })
    await screen.findByText('5 transactions affected')
    await fireEvent.input(screen.getByLabelText('Merchant name'), {
      target: { value: '  EXAMPLE MERCHANT  ' },
    })
    expect(screen.queryByRole('option', { name: /^Create/ })).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Save pending change' }))
    expect(controller.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        input: { scope: 'transactions', label: 'Example Merchant', destination_id: 'merchant-a' },
      }),
    )
  })
})
