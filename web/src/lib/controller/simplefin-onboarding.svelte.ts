import { createSubscriber } from 'svelte/reactivity'

import type { components } from '../api/schema'
import { createMutationFetch, MoneyflowProblem, requestProfileJSON } from '../api/client'
import { apiURL } from './base-path'

export type SimpleFINOnboardingStatus = components['schemas']['SimpleFINOnboardingStatusResponse']
type SimpleFINStartBody = components['schemas']['SimpleFINOnboardingStartBody']
type SimpleFINSubmitBody = components['schemas']['SimpleFINOnboardingSubmitBody']
type SimpleFINCancelBody = components['schemas']['SimpleFINOnboardingCancelBody']

export interface SimpleFINOnboardingTransport {
  start(profileID: string, body: SimpleFINStartBody): Promise<SimpleFINOnboardingStatus>
  submit(
    profileID: string,
    attemptID: string,
    body: SimpleFINSubmitBody,
  ): Promise<SimpleFINOnboardingStatus>
  cancel(
    profileID: string,
    attemptID: string,
    body: SimpleFINCancelBody,
  ): Promise<SimpleFINOnboardingStatus>
  status(profileID: string, attemptID: string): Promise<SimpleFINOnboardingStatus>
}

export interface SimpleFINOnboardingState {
  snapshot?: SimpleFINOnboardingStatus
  busy: boolean
  announcement: string
  problem?: { kind: 'start' | 'expired'; message: string } | undefined
}

export interface SimpleFINOnboardingController {
  readonly state: SimpleFINOnboardingState
  start(): Promise<void>
  connect(input: string, settings: SimpleFINSubmitBody['settings']): Promise<void>
  retry(): Promise<void>
  restart(): Promise<void>
  cancel(): Promise<void>
  poll(): Promise<void>
  destroy(): void
}

export function createSimpleFINOnboardingTransport(
  basePath: string,
  profileID: string,
  upstream: typeof fetch = fetch,
): SimpleFINOnboardingTransport {
  const prefix = `api/v1/profiles/${profileID}/simplefin-onboarding/`
  const mutations = createMutationFetch(
    basePath,
    upstream,
    null,
    Date.now,
    `api/v1/profiles/${profileID}/bootstrap`,
  )
  const mutation = <T>(path: string, body: unknown): Promise<T> =>
    requestProfileJSON<T>(mutations, `${prefix}${path}`, body)

  async function read<T>(path: string, valid: (value: unknown) => value is T): Promise<T> {
    const response = await upstream(apiURL(basePath, `${prefix}${path}`), {
      method: 'GET',
      cache: 'no-store',
      credentials: 'omit',
      redirect: 'error',
      headers: { Accept: 'application/json' },
    })
    const body: unknown = await response.json().catch(() => undefined)
    if (!response.ok) {
      if (isProblem(body)) throw new MoneyflowProblem(body)
      throw new Error('The SimpleFIN onboarding request failed.')
    }
    if (!valid(body)) throw new Error('The SimpleFIN onboarding response is invalid.')
    return body
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
    status(_profileID, attemptID) {
      return read(`${encodeURIComponent(attemptID)}`, isStatus)
    },
  }
}

export function createSimpleFINOnboardingController(options: {
  profileID: string
  transport: SimpleFINOnboardingTransport
  pollIntervalMS?: number
}): SimpleFINOnboardingController {
  let state: SimpleFINOnboardingState = { busy: false, announcement: '' }
  let notify = (): void => undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let destroyed = false
  let generation = 0
  const pollInterval = options.pollIntervalMS ?? 750
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => (notify = () => undefined)
  })

  function setState(next: SimpleFINOnboardingState): void {
    state = next
    notify()
    schedule()
  }

  function schedule(): void {
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
    if (destroyed || state.busy || state.problem || !isRunning(state.snapshot?.state)) return
    timer = setTimeout(() => void poll(), pollInterval)
  }

  function install(snapshot: SimpleFINOnboardingStatus): void {
    setState({ snapshot, busy: false, announcement: announcementFor(snapshot) })
  }

  async function apply(
    operation: () => Promise<SimpleFINOnboardingStatus>,
    pending: string,
  ): Promise<void> {
    if (destroyed || state.busy) return
    const requestGeneration = ++generation
    const active = state.snapshot
    const previousProblem = state.problem
    setState({ ...state, busy: true, announcement: pending, problem: undefined })
    try {
      const result = await operation()
      if (destroyed || generation !== requestGeneration) return
      install(result)
    } catch (error) {
      if (destroyed || generation !== requestGeneration) return
      if (
        error instanceof MoneyflowProblem &&
        error.problem.code === 'onboarding_stale' &&
        active
      ) {
        try {
          const result = await options.transport.status(options.profileID, active.attempt_id)
          if (destroyed || generation !== requestGeneration) return
          install(result)
          return
        } catch {
          // Report the original conflict if the authoritative attempt cannot be recovered.
        }
      }
      const message = problemMessage(error)
      const expired =
        error instanceof MoneyflowProblem && error.problem.code === 'onboarding_expired'
      setState({
        ...state,
        busy: false,
        announcement: message,
        ...(!active || expired || previousProblem
          ? {
              problem: {
                kind: expired ? ('expired' as const) : (previousProblem?.kind ?? 'start'),
                message,
              },
            }
          : {}),
      })
    }
  }

  async function start(): Promise<void> {
    await apply(
      () =>
        options.transport.start(options.profileID, {
          protocol_version: 1,
        }),
      'Checking SimpleFIN setup…',
    )
  }

  async function submit(
    body: Omit<SimpleFINSubmitBody, 'protocol_version' | 'expected_state_version'>,
  ) {
    const snapshot = state.snapshot
    if (!snapshot) return
    await apply(
      () =>
        options.transport.submit(options.profileID, snapshot.attempt_id, {
          protocol_version: 1,
          expected_state_version: snapshot.state_version,
          ...body,
        }),
      body.action !== 'connect' ? 'Retrying SimpleFIN setup…' : 'Continuing SimpleFIN setup…',
    )
  }

  async function poll(): Promise<void> {
    const snapshot = state.snapshot
    if (!snapshot || state.busy || state.problem || !isRunning(snapshot.state)) return
    if (typeof document !== 'undefined' && document.visibilityState !== 'visible') {
      schedule()
      return
    }
    const expectedGeneration = generation
    try {
      const next = await options.transport.status(options.profileID, snapshot.attempt_id)
      if (destroyed || generation !== expectedGeneration) return
      install(next)
    } catch (error) {
      if (destroyed || generation !== expectedGeneration) return
      if (error instanceof MoneyflowProblem && error.problem.code === 'onboarding_expired') {
        const message = error.problem.detail
        setState({
          ...state,
          busy: false,
          announcement: message,
          problem: { kind: 'expired', message },
        })
        return
      }
      setState({ ...state, announcement: 'Waiting for SimpleFIN setup status…' })
    }
  }

  return {
    get state() {
      subscribe()
      return state
    },
    start,
    connect: (input, settings) => submit({ action: 'connect', input, settings }),
    retry: () =>
      submit({
        action:
          state.snapshot?.failure?.code === 'session_save_failed' ? 'retry_save' : 'retry_import',
        settings: { currency: '', scale: 0 },
      }),
    restart: start,
    async cancel() {
      const snapshot = state.snapshot
      if (!snapshot) return
      await apply(
        () =>
          options.transport.cancel(options.profileID, snapshot.attempt_id, {
            protocol_version: 1,
            expected_state_version: snapshot.state_version,
          }),
        'Canceling SimpleFIN setup…',
      )
    },
    poll,
    destroy() {
      destroyed = true
      if (timer !== undefined) clearTimeout(timer)
    },
  }
}

function isRunning(state: string | undefined): boolean {
  return ['inspect', 'claiming', 'saving_session', 'importing'].includes(state ?? '')
}

function announcementFor(snapshot: SimpleFINOnboardingStatus): string {
  const names: Record<string, string> = {
    inspect: 'Checking saved SimpleFIN session…',
    credentials_required: 'Enter a SimpleFIN setup token or Access URL.',
    claiming: 'Claiming your SimpleFIN connection…',
    saving_session: 'Saving your connection before importing…',
    importing: 'Importing SimpleFIN data…',
    complete: 'SimpleFIN setup complete.',
    identity_mismatch: 'Use the original connection or create a new profile.',
    failed: snapshot.failure?.message ?? 'SimpleFIN setup failed.',
    canceled:
      snapshot.failure?.message ??
      'Setup stopped. Reopen this profile to use its saved connection.',
  }
  return names[snapshot.state] ?? 'SimpleFIN setup updated.'
}

function problemMessage(error: unknown): string {
  return error instanceof MoneyflowProblem
    ? error.problem.detail
    : 'The SimpleFIN onboarding request could not be completed.'
}

function isStatus(value: unknown): value is SimpleFINOnboardingStatus {
  return (
    isRecord(value) &&
    value.protocol_version === 1 &&
    typeof value.attempt_id === 'string' &&
    typeof value.profile_id === 'string' &&
    typeof value.state_version === 'number' &&
    typeof value.state === 'string' &&
    value.provider_kind === 'simplefin' &&
    isRecord(value.progress) &&
    typeof value.imported_transactions === 'number'
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
