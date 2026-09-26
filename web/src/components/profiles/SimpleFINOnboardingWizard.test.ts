import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, expect, it, vi } from 'vitest'
import {
  createSimpleFINOnboardingController,
  type SimpleFINOnboardingController,
  type SimpleFINOnboardingStatus,
} from '../../lib/controller/simplefin-onboarding.svelte'
import SimpleFINOnboardingWizard from './SimpleFINOnboardingWizard.svelte'

afterEach(cleanup)

it('keeps interrupted-claim recovery visible until explicitly dismissed', async () => {
  const claiming: SimpleFINOnboardingStatus = {
    protocol_version: 1,
    profile_id: 'profile_example',
    attempt_id: 'attempt_example',
    state_version: 3,
    state: 'claiming',
    provider_kind: 'simplefin',
    progress: { fetched: 0, total: 0 },
    imported_transactions: 0,
  }
  const message = 'If the connection was not saved, revoke it in SimpleFIN and obtain a new token.'
  const controller = createSimpleFINOnboardingController({
    profileID: claiming.profile_id,
    transport: {
      start: vi.fn(async () => claiming),
      status: vi.fn(async () => claiming),
      submit: vi.fn(),
      cancel: vi.fn(async () => ({
        ...claiming,
        state: 'canceled',
        state_version: 4,
        failure: { code: 'claim_interrupted', message, can_retry: false, can_reenter: false },
      })),
    },
  })
  await controller.start()
  const oncancel = vi.fn()
  render(SimpleFINOnboardingWizard, { controller, oncomplete: vi.fn(), oncancel })
  await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(oncancel).not.toHaveBeenCalled()
  expect(screen.getByRole('alert').textContent).toContain(message)
  expect(controller.state.announcement).toBe(message)
  await fireEvent.click(screen.getByRole('button', { name: 'Back to profiles' }))
  expect(oncancel).toHaveBeenCalledOnce()
})

it('confirms money settings before masked input and keeps invalid input for correction', async () => {
  const controller: SimpleFINOnboardingController = {
    state: {
      snapshot: {
        protocol_version: 1,
        profile_id: 'profile_example',
        attempt_id: 'attempt_example',
        state_version: 1,
        state: 'credentials_required',
        provider_kind: 'simplefin',
        progress: { fetched: 0, total: 0 },
        imported_transactions: 0,
      },
      busy: false,
      announcement: '',
    },
    start: vi.fn(),
    connect: vi.fn(),
    retry: vi.fn(),
    restart: vi.fn(),
    cancel: vi.fn(),
    poll: vi.fn(),
    destroy: vi.fn(),
  }
  const oncancel = vi.fn()
  render(SimpleFINOnboardingWizard, { controller, oncomplete: vi.fn(), oncancel })
  expect(screen.getByText(/Edits stay in Moneyflow/)).toBeTruthy()
  expect(screen.getByLabelText('Currency')).toBe(document.activeElement)
  await fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
  const input = screen.getByLabelText('Setup token or Access URL') as HTMLInputElement
  expect(input.type).toBe('password')
  expect(input).toBe(document.activeElement)
  await fireEvent.input(input, { target: { value: 'x'.repeat(8193) } })
  await fireEvent.submit(input.closest('form')!)
  expect(controller.connect).not.toHaveBeenCalled()
  expect(input.value.length).toBe(8193)
  await fireEvent.input(input, { target: { value: 'synthetic-token' } })
  await fireEvent.submit(input.closest('form')!)
  expect(controller.connect).toHaveBeenCalledWith('synthetic-token', { currency: 'USD', scale: 2 })
  expect(input.value).toBe('')
  await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(controller.cancel).toHaveBeenCalledOnce()
  expect(oncancel).not.toHaveBeenCalled()
})
