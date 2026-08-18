import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { OnboardingController } from '../../lib/controller/onboarding.svelte'
import OnboardingWizard from './OnboardingWizard.svelte'

describe('onboarding wizard', () => {
  afterEach(cleanup)

  it('uses password controls and clears secrets before submit completes', async () => {
    let resolve!: () => void
    const submitCredentials = vi.fn(
      () => new Promise<void>((done) => (resolve = done)),
    ) as OnboardingController['submitCredentials']
    const controller = stubController('credentials_required', { submitCredentials })
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel: vi.fn() })

    const password = screen.getByLabelText('Monarch password') as HTMLInputElement
    expect(password.type).toBe('password')
    await fireEvent.input(password, { target: { value: 'synthetic-secret' } })
    await fireEvent.input(screen.getByLabelText('Monarch email'), {
      target: { value: 'user@example.test' },
    })
    await fireEvent.input(screen.getByLabelText('TOTP secret'), {
      target: { value: 'JBSWY3DPEHPK3PXP' },
    })
    await fireEvent.input(screen.getByLabelText('Moneyflow account password'), {
      target: { value: 'vault-secret' },
    })
    await fireEvent.input(screen.getByLabelText('Confirm Moneyflow account password'), {
      target: { value: 'vault-secret' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    expect(password.value).toBe('')
    resolve()
  })

  it('shows progress counts and elapsed time, and separates retry from credential re-entry', async () => {
    const controller = stubController('failed', {
      snapshot: {
        ...snapshot('failed'),
        progress: {
          phase: 'fetching',
          partition: 'visible',
          fetched: 1000,
          total: 4000,
          attempt: 1,
          pass: 1,
          elapsed_ms: 2500,
        },
        failure: {
          code: 'provider_data_invalid',
          message: 'The provider data could not be imported.',
          can_retry: true,
          can_reenter: true,
        },
      },
    })
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel: vi.fn() })

    expect(screen.getByRole('status').textContent).toContain('1,000 of 4,000')
    expect(screen.getByRole('status').textContent).toContain('2.5 seconds')
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(controller.retry).toHaveBeenCalledTimes(1)
    await fireEvent.click(screen.getByRole('button', { name: 'Re-enter credentials' }))
    expect(controller.reauthenticate).toHaveBeenCalledTimes(1)
  })

  it('waits for the canceled coordinator state before leaving the wizard', async () => {
    const oncancel = vi.fn()
    const controller = stubController('settings_required')
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel })

    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(controller.cancel).toHaveBeenCalledTimes(1)
    expect(oncancel).not.toHaveBeenCalled()
  })
})

function snapshot(state: string) {
  return {
    protocol_version: 1,
    attempt_id: 'attempt_synthetic',
    profile_id: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa',
    state_version: 1,
    state,
    provider_kind: 'monarch',
  }
}

function stubController(
  state: string,
  overrides: Partial<OnboardingController> & {
    snapshot?: OnboardingController['state']['snapshot']
  } = {},
): OnboardingController {
  return {
    state: {
      snapshot: overrides.snapshot ?? snapshot(state),
      busy: false,
      announcement: '',
    },
    start: vi.fn(async () => undefined),
    confirmSettings: vi.fn(async () => undefined),
    unlock: vi.fn(async () => undefined),
    submitCredentials: vi.fn(async () => undefined),
    retry: vi.fn(async () => undefined),
    reauthenticate: vi.fn(async () => undefined),
    cancel: vi.fn(async () => undefined),
    poll: vi.fn(async () => undefined),
    destroy: vi.fn(),
    ...overrides,
  } as OnboardingController
}
