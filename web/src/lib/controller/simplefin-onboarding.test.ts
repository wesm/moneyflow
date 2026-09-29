import { afterEach, expect, it, vi } from 'vitest'
import {
  createSimpleFINOnboardingController,
  type SimpleFINOnboardingStatus,
  type SimpleFINOnboardingTransport,
} from './simplefin-onboarding.svelte'

afterEach(() => vi.useRealTimers())

it('guards connect and retries import without sending the credential again', async () => {
  vi.useFakeTimers()
  const snapshot: SimpleFINOnboardingStatus = {
    protocol_version: 1,
    profile_id: 'profile_example',
    attempt_id: 'attempt_example',
    state_version: 2,
    state: 'credentials_required',
    provider_kind: 'simplefin',
    progress: { fetched: 0, total: 0 },
    imported_transactions: 0,
  }
  const transport: SimpleFINOnboardingTransport = {
    start: vi.fn(async () => snapshot),
    status: vi.fn(async () => snapshot),
    submit: vi.fn(async () => ({
      ...snapshot,
      state: 'failed',
      state_version: 4,
      failure: {
        code: 'provider_unavailable',
        message: 'Retry later',
        can_retry: true,
        can_reenter: false,
      },
    })),
    cancel: vi.fn(async () => ({ ...snapshot, state: 'canceled' })),
  }
  const controller = createSimpleFINOnboardingController({
    profileID: snapshot.profile_id,
    transport,
  })
  await controller.start()
  await controller.connect('synthetic-token', { currency: 'USD', scale: 2 })
  expect(transport.submit).toHaveBeenLastCalledWith(snapshot.profile_id, snapshot.attempt_id, {
    protocol_version: 1,
    expected_state_version: 2,
    action: 'connect',
    input: 'synthetic-token',
    settings: { currency: 'USD', scale: 2 },
  })
  await controller.retry()
  expect(transport.submit).toHaveBeenLastCalledWith(snapshot.profile_id, snapshot.attempt_id, {
    protocol_version: 1,
    expected_state_version: 4,
    action: 'retry_import',
    settings: { currency: '', scale: 0 },
  })
  expect(JSON.stringify(controller.state)).not.toContain('synthetic-token')
  controller.destroy()
})

it('ignores a late start response after the wizard is destroyed', async () => {
  let finish!: (value: SimpleFINOnboardingStatus) => void
  const transport = {
    start: vi.fn(() => new Promise<SimpleFINOnboardingStatus>((resolve) => (finish = resolve))),
    status: vi.fn(),
    submit: vi.fn(),
    cancel: vi.fn(),
  }
  const controller = createSimpleFINOnboardingController({
    profileID: 'profile_example',
    transport,
  })
  const pending = controller.start()
  controller.destroy()
  finish({
    protocol_version: 1,
    profile_id: 'profile_example',
    attempt_id: 'attempt_example',
    state_version: 1,
    state: 'complete',
    provider_kind: 'simplefin',
    progress: { fetched: 0, total: 0 },
    imported_transactions: 0,
  })
  await pending
  expect(controller.state.snapshot).toBeUndefined()
})
