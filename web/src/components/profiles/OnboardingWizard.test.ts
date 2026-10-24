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
    await fireEvent.input(screen.getByLabelText('Authenticator setup key'), {
      target: { value: 'JBSWY3DPEHPK3PXP' },
    })
    await fireEvent.input(screen.getByLabelText('Account password'), {
      target: { value: 'vault-secret' },
    })
    await fireEvent.input(screen.getByLabelText('Confirm account password'), {
      target: { value: 'vault-secret' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    expect(password.value).toBe('')
    resolve()
  })

  it('describes the authenticator setup key and the new account password', () => {
    const controller = stubController('credentials_required')
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel: vi.fn() })

    expect(description(screen.getByLabelText('Authenticator setup key'))).toMatch(
      /not a six-digit code/,
    )
    expect(description(screen.getByLabelText('Account password'))).toMatch(
      /unlock this profile later/,
    )
  })

  it('shows why a step failed inside the panel and keeps the email for another try', async () => {
    const controller = stubController('credentials_required')
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel: vi.fn() })
    await fireEvent.input(screen.getByLabelText('Monarch email'), {
      target: { value: 'user@example.test' },
    })
    await fireEvent.input(screen.getByLabelText('Monarch password'), {
      target: { value: 'synthetic-secret' },
    })
    await fireEvent.input(screen.getByLabelText('Authenticator setup key'), {
      target: { value: 'JBSWY3DPEHPK3PXP' },
    })
    await fireEvent.input(screen.getByLabelText('Account password'), {
      target: { value: 'vault-secret' },
    })
    await fireEvent.input(screen.getByLabelText('Confirm account password'), {
      target: { value: 'vault-secret' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    expect((screen.getByLabelText('Monarch email') as HTMLInputElement).value).toBe(
      'user@example.test',
    )
    expect((screen.getByLabelText('Monarch password') as HTMLInputElement).value).toBe('')
  })

  it('renders the reason for a failed step as an alert in the panel', () => {
    const controller = stubController('credentials_required')
    controller.state.failure = 'Setup did not continue. Monarch rejected these credentials.'
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel: vi.fn() })

    expect(screen.getByRole('alert').textContent).toBe(
      'Setup did not continue. Monarch rejected these credentials.',
    )
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

  it('uses masked YNAB inputs, selects a budget by keyboard, and confirms derived money', async () => {
    const controller = stubController('credentials_required', {
      snapshot: { ...snapshot('credentials_required'), provider_kind: 'ynab' },
    })
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel: vi.fn() })

    const token = screen.getByLabelText('YNAB personal access token') as HTMLInputElement
    expect(token.type).toBe('password')
    await fireEvent.input(token, { target: { value: 'synthetic-token' } })
    await fireEvent.input(screen.getByLabelText('Account password'), {
      target: { value: 'vault-secret' },
    })
    await fireEvent.input(screen.getByLabelText('Confirm account password'), {
      target: { value: 'vault-secret' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    expect(controller.submitYNABCredentials).toHaveBeenCalledWith({
      access_token: 'synthetic-token',
      account_password: 'vault-secret',
      confirmation: 'vault-secret',
    })
    expect(token.value).toBe('')

    cleanup()
    controller.state.snapshot = {
      ...snapshot('remote_profile_required'),
      provider_kind: 'ynab',
      remote_profiles: [
        { choice_id: 'choice_alpha', display_name: 'Alpha Budget' },
        { choice_id: 'choice_beta', display_name: 'Beta Budget' },
      ],
    }
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel: vi.fn() })
    const beta = screen.getByRole('button', { name: /Beta Budget/ })
    await fireEvent.click(beta)
    expect(controller.selectRemoteProfile).toHaveBeenCalledWith('choice_beta')

    cleanup()
    controller.state.snapshot = {
      ...snapshot('settings_required'),
      provider_kind: 'ynab',
      settings: { currency: 'EUR', scale: 2 },
    }
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel: vi.fn() })
    const currency = screen.getByLabelText('Currency') as HTMLInputElement
    expect(currency.readOnly).toBe(true)
    expect(currency.value).toBe('EUR')
    await fireEvent.click(screen.getByRole('button', { name: 'Continue with EUR / 2' }))
    expect(controller.confirmSettings).toHaveBeenCalledWith('EUR', 2)
  })

  it('waits for the canceled coordinator state before leaving the wizard', async () => {
    const oncancel = vi.fn()
    const controller = stubController('settings_required')
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel })

    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(controller.cancel).toHaveBeenCalledTimes(1)
    expect(oncancel).not.toHaveBeenCalled()
  })

  it('lets the user cancel while it checks a saved session', async () => {
    const controller = stubController('validate_session')
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel: vi.fn() })

    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(controller.cancel).toHaveBeenCalledTimes(1)
  })

  it('offers restart and back when setup cannot start', async () => {
    const oncancel = vi.fn()
    const controller = stubController('settings_required')
    delete controller.state.snapshot
    controller.state.problem = { kind: 'start', message: 'Setup could not start.' }
    render(OnboardingWizard, { controller, oncomplete: vi.fn(), oncancel })

    expect(screen.getByRole('heading', { name: 'Profile setup was interrupted' })).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Retry setup' }))
    expect(controller.restart).toHaveBeenCalledTimes(1)
    await fireEvent.click(screen.getByRole('button', { name: 'Back to profiles' }))
    expect(oncancel).toHaveBeenCalledTimes(1)
  })
})

function snapshot(state: string) {
  return {
    protocol_version: 2,
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
    submitYNABCredentials: vi.fn(async () => undefined),
    selectRemoteProfile: vi.fn(async () => undefined),
    retry: vi.fn(async () => undefined),
    reauthenticate: vi.fn(async () => undefined),
    cancel: vi.fn(async () => undefined),
    poll: vi.fn(async () => undefined),
    restart: vi.fn(async () => undefined),
    destroy: vi.fn(),
    ...overrides,
  } as OnboardingController
}

/** The text that an input's aria-describedby points to. */
function description(input: HTMLElement): string {
  const id = input.getAttribute('aria-describedby') ?? ''
  return (document.getElementById(id)?.textContent ?? '').replace(/\s+/g, ' ').trim()
}
