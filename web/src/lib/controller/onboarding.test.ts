import { describe, expect, it, vi } from 'vitest'

import { MoneyflowProblem } from '../api/client'
import type { OnboardingStatus } from './onboarding.svelte'
import { createOnboardingController } from './onboarding.svelte'

const profileID = 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa'

describe('onboarding controller', () => {
  it('starts with USD/2 defaults and submits state-versioned settings', async () => {
    const transport = stubTransport(status('settings_required'))
    const controller = createOnboardingController({ profileID, transport })

    await controller.start()
    await controller.confirmSettings('USD', 2)

    expect(transport.submit).toHaveBeenCalledWith(
      profileID,
      'attempt_synthetic',
      expect.objectContaining({
        protocol_version: 1,
        expected_state_version: 1,
        action: 'confirm_settings',
        settings: { currency: 'USD', scale: 2 },
      }),
    )
  })

  it('keeps secrets out of observable state and distinguishes retry from reauthentication', async () => {
    const transport = stubTransport(status('credentials_required'))
    const controller = createOnboardingController({ profileID, transport })

    await controller.start()
    await controller.submitCredentials({
      email: 'user@example.test',
      password: 'synthetic-secret',
      totp_secret: 'JBSWY3DPEHPK3PXP',
      account_password: 'vault-secret',
      confirmation: 'vault-secret',
    })

    expect(JSON.stringify(controller.state)).not.toContain('synthetic-secret')
    expect(JSON.stringify(controller.state)).not.toContain('vault-secret')
    expect(transport.submit).toHaveBeenCalledTimes(1)
  })

  it('polls running jobs and exposes counts and elapsed time only', async () => {
    vi.useFakeTimers()
    try {
      const importing = status('importing', {
        progress: {
          phase: 'fetching',
          partition: 'visible',
          fetched: 1000,
          total: 4000,
          attempt: 1,
          pass: 1,
          elapsed_ms: 2500,
        },
      })
      const transport = stubTransport(importing)
      const controller = createOnboardingController({ profileID, transport, pollIntervalMS: 50 })

      await controller.start()
      await vi.advanceTimersByTimeAsync(50)

      expect(transport.status).toHaveBeenCalled()
      expect(controller.state.snapshot?.progress).toMatchObject({ fetched: 1000, total: 4000 })
      controller.destroy()
    } finally {
      vi.useRealTimers()
    }
  })

  it('resynchronizes from status after another presenter advances the attempt', async () => {
    const initial = status('settings_required')
    const current = status('credentials_required', { state_version: 2 })
    const transport = stubTransport(initial)
    transport.submit.mockRejectedValueOnce(
      new MoneyflowProblem({
        type: 'about:blank',
        title: 'Onboarding state changed',
        status: 409,
        detail: 'The onboarding attempt changed in another presenter.',
        code: 'onboarding_stale',
      }),
    )
    transport.status.mockResolvedValueOnce(current)
    const controller = createOnboardingController({ profileID, transport })

    await controller.start()
    await controller.confirmSettings('USD', 2)

    expect(transport.status).toHaveBeenCalledWith(profileID, 'attempt_synthetic')
    expect(controller.state.snapshot).toEqual(current)
  })

  it('surfaces an initial start failure with an explicit restart path', async () => {
    const transport = stubTransport(status('settings_required'))
    transport.start.mockRejectedValueOnce(new Error('synthetic failure'))
    const controller = createOnboardingController({ profileID, transport })

    await controller.start()

    expect(controller.state.problem?.kind).toBe('start')
    await controller.restart()
    expect(transport.start).toHaveBeenCalledTimes(2)
    expect(controller.state.problem).toBeUndefined()
  })

  it('ignores a stale expired poll that races a successful restart', async () => {
    vi.useFakeTimers()
    try {
      const transport = stubTransport(status('importing'))
      const expired = new MoneyflowProblem({
        type: 'about:blank',
        title: 'Onboarding expired',
        status: 404,
        detail: 'The onboarding attempt expired.',
        code: 'onboarding_expired',
      })
      const controller = createOnboardingController({ profileID, transport, pollIntervalMS: 10 })
      await controller.start()

      let rejectStalePoll: (reason: unknown) => void = () => undefined
      transport.status.mockImplementationOnce(
        () => new Promise<OnboardingStatus>((_resolve, reject) => (rejectStalePoll = reject)),
      )
      await vi.advanceTimersByTimeAsync(10)
      expect(transport.status).toHaveBeenCalledTimes(1)
      transport.set(status('settings_required', { attempt_id: 'attempt_second' }))
      await controller.restart()
      expect(controller.state.snapshot?.attempt_id).toBe('attempt_second')

      rejectStalePoll(expired)
      await vi.advanceTimersByTimeAsync(0)
      expect(controller.state.snapshot?.attempt_id).toBe('attempt_second')
      expect(controller.state.problem).toBeUndefined()
      controller.destroy()
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not keep polling an expired attempt and clears the problem on restart', async () => {
    vi.useFakeTimers()
    try {
      const transport = stubTransport(status('importing'))
      const expired = new MoneyflowProblem({
        type: 'about:blank',
        title: 'Onboarding expired',
        status: 404,
        detail: 'The onboarding attempt expired.',
        code: 'onboarding_expired',
      })
      const controller = createOnboardingController({ profileID, transport, pollIntervalMS: 10 })
      await controller.start()
      transport.status.mockRejectedValue(expired)

      await vi.advanceTimersByTimeAsync(10)
      expect(controller.state.problem?.kind).toBe('expired')
      expect(transport.status).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(50)
      expect(transport.status).toHaveBeenCalledTimes(1)

      transport.set(status('settings_required', { attempt_id: 'attempt_second' }))
      await controller.restart()
      expect(controller.state.problem).toBeUndefined()
      expect(controller.state.snapshot?.attempt_id).toBe('attempt_second')
      await vi.advanceTimersByTimeAsync(50)
      expect(transport.status).toHaveBeenCalledTimes(1)
      controller.destroy()
    } finally {
      vi.useRealTimers()
    }
  })

  it('resumes polling when a hidden tab becomes visible', async () => {
    vi.useFakeTimers()
    const importing = status('importing')
    const transport = stubTransport(importing)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    const controller = createOnboardingController({ profileID, transport, pollIntervalMS: 10 })
    try {
      await controller.start()
      transport.status.mockClear()
      await vi.advanceTimersByTimeAsync(10)
      expect(transport.status).not.toHaveBeenCalled()
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
      await vi.advanceTimersByTimeAsync(10)
      expect(transport.status).toHaveBeenCalledTimes(1)
    } finally {
      controller.destroy()
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
      vi.useRealTimers()
    }
  })
})

function status(state: string, overrides: Partial<OnboardingStatus> = {}): OnboardingStatus {
  return {
    protocol_version: 1,
    attempt_id: 'attempt_synthetic',
    profile_id: profileID,
    state_version: 1,
    state,
    provider_kind: 'monarch',
    ...overrides,
  }
}

function stubTransport(initial: OnboardingStatus) {
  let current = initial
  return {
    start: vi.fn(async () => current),
    submit: vi.fn(async () => current),
    cancel: vi.fn(async () => ({ ...current, state: 'canceled' })),
    status: vi.fn(async () => current),
    set(next: OnboardingStatus) {
      current = next
    },
  }
}
