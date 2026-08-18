import { createSubscriber } from 'svelte/reactivity'

import type { components } from '../api/schema'
import { createMutationFetch, MoneyflowProblem, requestProfileJSON } from '../api/client'
import { apiURL } from './base-path'

export type OnboardingStatus = components['schemas']['OnboardingStatusResponse']
export type OnboardingSettings = components['schemas']['OnboardingSettingsInput']
export type OnboardingCredentials = components['schemas']['OnboardingCredentialsInput']
type OnboardingStartBody = components['schemas']['OnboardingStartBody']
type OnboardingSubmitBody = components['schemas']['OnboardingSubmitBody']
type OnboardingCancelBody = components['schemas']['OnboardingCancelBody']

export interface OnboardingTransport {
  start(profileID: string, body: OnboardingStartBody): Promise<OnboardingStatus>
  submit(
    profileID: string,
    attemptID: string,
    body: OnboardingSubmitBody,
  ): Promise<OnboardingStatus>
  cancel(
    profileID: string,
    attemptID: string,
    body: OnboardingCancelBody,
  ): Promise<OnboardingStatus>
  status(profileID: string, attemptID: string): Promise<OnboardingStatus>
}

export interface OnboardingState {
  snapshot?: OnboardingStatus
  busy: boolean
  announcement: string
  problem?: { kind: 'start' | 'expired'; message: string }
}

export interface OnboardingController {
  readonly state: OnboardingState
  start(settings?: OnboardingSettings): Promise<void>
  confirmSettings(currency: string, scale: number): Promise<void>
  unlock(accountPassword: string): Promise<void>
  submitCredentials(credentials: OnboardingCredentials): Promise<void>
  retry(): Promise<void>
  restart(): Promise<void>
  reauthenticate(): Promise<void>
  cancel(): Promise<void>
  poll(): Promise<void>
  destroy(): void
}

export function createOnboardingTransport(
  basePath: string,
  profileID: string,
  upstream: typeof fetch = fetch,
): OnboardingTransport {
  const prefix = `api/v1/profiles/${profileID}/onboarding/`
  const mutations = createMutationFetch(
    basePath,
    upstream,
    null,
    Date.now,
    `api/v1/profiles/${profileID}/bootstrap`,
  )

  async function mutation<T>(path: string, body: unknown): Promise<T> {
    return requestProfileJSON<T>(mutations, `${prefix}${path}`, body)
  }

  return {
    start(_profileID, body) {
      return mutation('start', body)
    },
    submit(_profileID, attemptID, body) {
      return mutation(`${encodeURIComponent(attemptID)}/submit`, body)
    },
    cancel(_profileID, attemptID, body) {
      return mutation(`${encodeURIComponent(attemptID)}/cancel`, body)
    },
    async status(_profileID, attemptID) {
      const response = await upstream(
        apiURL(basePath, `${prefix}${encodeURIComponent(attemptID)}/status`),
        {
          method: 'GET',
          cache: 'no-store',
          credentials: 'omit',
          redirect: 'error',
          headers: { Accept: 'application/json' },
        },
      )
      const body: unknown = await response.json().catch(() => undefined)
      if (!response.ok) {
        if (isProblem(body)) throw new MoneyflowProblem(body)
        throw new Error('The onboarding status request failed.')
      }
      if (!isStatus(body)) throw new Error('The onboarding status response is invalid.')
      return body
    },
  }
}

export function createOnboardingController(options: {
  profileID: string
  transport: OnboardingTransport
  pollIntervalMS?: number
}): OnboardingController {
  let state: OnboardingState = { busy: false, announcement: '' }
  let notify = (): void => undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let destroyed = false
  let generation = 0
  const pollInterval = options.pollIntervalMS ?? 750
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => (notify = () => undefined)
  })

  function setState(next: OnboardingState): void {
    state = next
    notify()
    schedule()
  }

  function schedule(): void {
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
    if (destroyed || state.problem || !isRunning(state.snapshot?.state)) return
    timer = setTimeout(() => void poll(), pollInterval)
  }

  async function apply(operation: () => Promise<OnboardingStatus>, pending: string): Promise<void> {
    if (state.busy) return
    generation += 1
    const activeSnapshot = state.snapshot
    setState({
      busy: true,
      announcement: pending,
      ...(activeSnapshot ? { snapshot: activeSnapshot } : {}),
    })
    try {
      const snapshot = await operation()
      setState({ snapshot, busy: false, announcement: announcementFor(snapshot) })
    } catch (error) {
      if (error instanceof MoneyflowProblem && error.problem.code === 'onboarding_stale') {
        if (activeSnapshot) {
          try {
            const snapshot = await options.transport.status(
              options.profileID,
              activeSnapshot.attempt_id,
            )
            setState({ snapshot, busy: false, announcement: announcementFor(snapshot) })
            return
          } catch {
            // Surface the original conflict when the authoritative status cannot be recovered.
          }
        }
      }
      const message =
        error instanceof MoneyflowProblem
          ? error.problem.detail
          : 'The onboarding request could not be completed.'
      const expired =
        error instanceof MoneyflowProblem && error.problem.code === 'onboarding_expired'
      setState({
        ...state,
        busy: false,
        announcement: message,
        ...(!activeSnapshot || expired
          ? { problem: { kind: expired ? ('expired' as const) : ('start' as const), message } }
          : {}),
      })
    }
  }

  async function start(settings?: OnboardingSettings): Promise<void> {
    await apply(
      () =>
        options.transport.start(options.profileID, {
          protocol_version: 1,
          month_to_date: false,
          ...(settings ? { settings } : {}),
        }),
      'Checking profile setup…',
    )
  }

  async function submit(
    action: string,
    payload: Pick<OnboardingSubmitBody, 'settings' | 'unlock' | 'credentials'> = {},
  ): Promise<void> {
    const snapshot = state.snapshot
    if (!snapshot) return
    await apply(
      () =>
        options.transport.submit(options.profileID, snapshot.attempt_id, {
          protocol_version: 1,
          expected_state_version: snapshot.state_version,
          action,
          ...payload,
        }),
      action === 'retry' ? 'Retrying provider setup…' : 'Continuing provider setup…',
    )
  }

  async function poll(): Promise<void> {
    const snapshot = state.snapshot
    if (!snapshot || state.problem || !isRunning(snapshot.state)) return
    if (!pageVisible()) {
      schedule()
      return
    }
    const expectedGeneration = generation
    const stale = (): boolean =>
      destroyed ||
      generation !== expectedGeneration ||
      state.snapshot?.attempt_id !== snapshot.attempt_id
    try {
      const next = await options.transport.status(options.profileID, snapshot.attempt_id)
      if (stale()) return
      setState({ snapshot: next, busy: false, announcement: announcementFor(next) })
    } catch (error) {
      if (stale()) return
      if (error instanceof MoneyflowProblem && error.problem.code === 'onboarding_expired') {
        setState({
          ...state,
          busy: false,
          announcement: error.problem.detail,
          problem: { kind: 'expired', message: error.problem.detail },
        })
        return
      }
      setState({ ...state, announcement: 'Waiting for provider setup status…' })
    }
  }

  async function cancel(): Promise<void> {
    const snapshot = state.snapshot
    if (!snapshot) return
    await apply(
      () =>
        options.transport.cancel(options.profileID, snapshot.attempt_id, {
          protocol_version: 1,
          expected_state_version: snapshot.state_version,
        }),
      'Canceling provider setup…',
    )
  }

  return {
    get state() {
      subscribe()
      return state
    },
    start,
    confirmSettings: (currency, scale) =>
      submit('confirm_settings', { settings: { currency, scale } }),
    unlock: (accountPassword) =>
      submit('unlock', { unlock: { account_password: accountPassword } }),
    submitCredentials: (credentials) => submit('submit_credentials', { credentials }),
    retry: () => submit('retry'),
    restart: () => start(state.snapshot?.settings),
    reauthenticate: () => submit('reauthenticate'),
    cancel,
    poll,
    destroy() {
      destroyed = true
      if (timer !== undefined) clearTimeout(timer)
    },
  }
}

function isRunning(state: string | undefined): boolean {
  return ['inspect', 'validate_session', 'authenticating', 'importing'].includes(state ?? '')
}

function pageVisible(): boolean {
  return typeof document === 'undefined' || document.visibilityState === 'visible'
}

function announcementFor(snapshot: OnboardingStatus): string {
  const names: Record<string, string> = {
    inspect: 'Inspecting profile…',
    validate_session: 'Checking saved session…',
    settings_required: 'Confirm import settings.',
    unlock_required: 'Unlock saved Monarch credentials.',
    credentials_required: 'Enter Monarch credentials.',
    authenticating: 'Authenticating with Monarch…',
    importing: 'Importing Monarch data…',
    complete: 'Monarch setup complete.',
    local_only: 'This profile can be opened offline.',
    identity_mismatch: 'This profile is bound to a different Monarch account.',
    failed: snapshot.failure?.message ?? 'Provider setup failed.',
    canceled: 'Provider setup canceled.',
  }
  return names[snapshot.state] ?? 'Provider setup updated.'
}

function isStatus(value: unknown): value is OnboardingStatus {
  return (
    isRecord(value) &&
    value.protocol_version === 1 &&
    typeof value.attempt_id === 'string' &&
    typeof value.profile_id === 'string' &&
    typeof value.state_version === 'number' &&
    typeof value.state === 'string' &&
    typeof value.provider_kind === 'string'
  )
}

function isProblem(value: unknown): value is components['schemas']['Problem'] {
  return (
    isRecord(value) &&
    typeof value.type === 'string' &&
    typeof value.title === 'string' &&
    typeof value.status === 'number' &&
    typeof value.detail === 'string' &&
    typeof value.code === 'string'
  )
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
