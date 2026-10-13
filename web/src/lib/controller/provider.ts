import { showFlash } from '@kenn-io/kit-ui'
import { createSubscriber } from 'svelte/reactivity'

import {
  MoneyflowProblem,
  requestProfileJSON,
  type MutationFetch,
  type Problem,
  type ProviderConfirmationBody,
  type ProviderRefreshBody,
  type ProviderRefreshResponse,
  type ProviderStatus,
  type ViewProjection,
} from '../api/client'

type Capability = NonNullable<ViewProjection['capabilities']>[number]
export type ProviderPhase = 'idle' | 'refreshing' | 'confirmation' | 'reconnect' | 'failed'
/** What a running Monarch refresh reads: the last 90 days, or complete history. */
export type RefreshKind = 'recent' | 'full'

export interface ProviderState {
  phase: ProviderPhase
  announcement: string
  capability: Capability | undefined
  status?: ProviderStatus
  kind?: RefreshKind | undefined
}

export interface ProviderController {
  readonly state: ProviderState
  sync(projection: ViewProjection): void
  poll(): Promise<void>
  refresh(): Promise<boolean>
  confirm(): Promise<boolean>
  dismissConfirmation(): void
}

export interface ProviderTransport {
  mutations: MutationFetch
  status(signal?: AbortSignal): Promise<ProviderStatus>
}

interface ProviderProjectionHost {
  current(): ViewProjection | undefined
  accept(projection: ViewProjection): void
  /** Rereads the shown view at the newest revision. */
  reload(): Promise<unknown>
  /** Runs `work` once the view changes already on their way have finished. */
  serially<T>(work: () => Promise<T>): Promise<T>
}

interface ProviderControllerOptions {
  transport: ProviderTransport
  host: ProviderProjectionHost
}

/** Status polling interval while a refresh runs, so progress and the outcome appear promptly. */
const refreshingPollMillis = 1_000
/** A failure banner stays long enough to read a full sentence; it can be dismissed sooner. */
const failureFlashMillis = 12_000
/** The server logs the cause of an unexpected error; the browser only says where to look. */
const serverLogHint = 'The terminal running moneyflow web shows the cause.'

export function createProviderController(options: ProviderControllerOptions): ProviderController {
  let state: ProviderState = { phase: 'idle', announcement: '', capability: undefined }
  let inFlight = false
  let settled = 0
  let refreshingPoll: ReturnType<typeof setInterval> | undefined
  let notify = (): void => undefined
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => {
      notify = () => undefined
    }
  })

  function setState(next: ProviderState): void {
    state = next
    if (state.phase === 'refreshing' && refreshingPoll === undefined) {
      refreshingPoll = setInterval(() => void poll(), refreshingPollMillis)
    } else if (state.phase !== 'refreshing' && refreshingPoll !== undefined) {
      clearInterval(refreshingPoll)
      refreshingPoll = undefined
    }
    notify()
  }

  function refreshKind(manual: boolean): RefreshKind | undefined {
    const status = state.status
    if (status?.provider_kind !== 'monarch') return undefined
    if (manual) return 'full'
    return status.refresh_due === 'none' ? undefined : status.refresh_due
  }

  function sync(projection: ViewProjection): void {
    const capability = projection.capabilities?.find(({ id }) => id === 'provider.refresh')
    setState({ ...state, capability })
  }

  async function refresh(manual = true): Promise<boolean> {
    const current = options.host.current()
    if (!current) return false
    const capability = state.capability
    if (!capability?.available) {
      setState({
        ...state,
        announcement: capability?.reason ?? 'Provider refresh is unavailable.',
      })
      return false
    }
    if (inFlight) return false

    inFlight = true
    setState({
      ...state,
      phase: 'refreshing',
      kind: refreshKind(manual),
      announcement: 'Refreshing provider data…',
    })
    const body: ProviderRefreshBody = {
      version: '1',
      manual,
      query: current.canonical_query,
      selection: current.selection,
      window: { offset: current.window.offset, limit: current.window.limit },
    }
    try {
      return await submit('api/v1/provider/refresh', body)
    } finally {
      inFlight = false
    }
  }

  async function confirm(): Promise<boolean> {
    const current = options.host.current()
    const token = state.status?.confirmation_token
    if (!current || state.phase !== 'confirmation' || !token || inFlight) return false
    inFlight = true
    setState({
      ...state,
      phase: 'refreshing',
      announcement: 'Applying confirmed provider refresh…',
    })
    const body: ProviderConfirmationBody = {
      version: '1',
      manual: true,
      query: current.canonical_query,
      selection: current.selection,
      window: { offset: current.window.offset, limit: current.window.limit },
      confirmation_token: token,
    }
    try {
      return await submit('api/v1/provider/refresh/confirm', body, true)
    } finally {
      inFlight = false
    }
  }

  async function submit(
    path: string,
    body: ProviderRefreshBody | ProviderConfirmationBody,
    confirmation = false,
  ): Promise<boolean> {
    const current = options.host.current()
    if (!current) return false
    let response: ProviderRefreshResponse
    try {
      response = await requestProfileJSON<ProviderRefreshResponse>(
        options.transport.mutations,
        path,
        body,
      )
    } catch (error) {
      settled++
      return fail(error, confirmation)
    }
    settled++
    if (response.projection.canonical_query !== current.canonical_query) {
      return fail(new Error('The provider refresh changed the analytical view.'), confirmation)
    }
    const projection = {
      ...response.projection,
      selection: response.selection.value,
    } as ViewProjection
    const complete = await show(projection, current, response.selection.kind === 'cleared')
    setState({
      phase: 'idle',
      capability: state.capability,
      status: response.status,
      announcement: [complete, response.status.warning].filter(Boolean).join(' '),
    })
    return true
  }

  // A view or selection change made while the refresh ran stays: wait for any still on its way,
  // so installing the refreshed view does not cancel it, then reread the view it produced at the
  // refreshed revision rather than reinstall the view the refresh was sent with.
  function show(
    projection: ViewProjection,
    sent: ViewProjection,
    selectionCleared: boolean,
  ): Promise<string> {
    return options.host.serially(() => showSettled(projection, sent, selectionCleared))
  }

  async function showSettled(
    projection: ViewProjection,
    sent: ViewProjection,
    selectionCleared: boolean,
  ): Promise<string> {
    const shown = options.host.current()
    if (
      !shown ||
      (shown.canonical_query === sent.canonical_query && shown.selection === sent.selection)
    ) {
      options.host.accept(projection)
      return selectionCleared
        ? 'Provider refresh complete. Selection cleared because selected rows changed.'
        : 'Provider refresh complete.'
    }
    try {
      await options.host.reload()
      return 'Provider refresh complete.'
    } catch {
      return 'Provider refresh complete. The current view could not be reread.'
    }
  }

  async function fail(error: unknown, confirmation: boolean): Promise<boolean> {
    if (!(error instanceof MoneyflowProblem)) {
      const announcement = 'Provider refresh failed. Press r to try again.'
      setState({ ...state, phase: 'failed', kind: undefined, announcement })
      showFlash(announcement, { tone: 'danger', durationMs: failureFlashMillis })
      return false
    }
    const problem = error.problem
    if (problem.code === 'provider_deletion_confirmation_required' && problem.provider) {
      setState({
        phase: 'confirmation',
        capability: state.capability,
        status: problem.provider,
        announcement: problem.detail,
      })
      return false
    }
    showFlash(`Provider refresh failed: ${failureReason(problem)}`, {
      tone: 'danger',
      durationMs: failureFlashMillis,
    })
    if (problem.code === 'provider_reconnect_required') {
      setState({ ...state, phase: 'reconnect', kind: undefined, announcement: problem.detail })
      return false
    }
    if (confirmation && problem.code === 'provider_confirmation_invalid') {
      await refreshStatusAfterInvalidConfirmation(problem.detail)
      return false
    }
    setState({
      ...state,
      phase: 'failed',
      kind: undefined,
      ...(problem.provider === undefined ? {} : { status: problem.provider }),
      announcement: failureReason(problem),
    })
    return false
  }

  async function refreshStatusAfterInvalidConfirmation(detail: string): Promise<void> {
    try {
      const status = await options.transport.status()
      setState({
        phase: 'failed',
        capability: status.capability,
        status,
        announcement: `${detail} Press r to fetch a new candidate.`,
      })
    } catch {
      setState({ ...state, phase: 'failed', announcement: detail })
    }
  }

  async function poll(): Promise<void> {
    if (state.phase === 'confirmation') return
    const seen = settled
    try {
      const status = await options.transport.status()
      // A refresh that finished meanwhile returned newer status than this poll read.
      if (seen !== settled) return
      const phase = statusPhase(status, inFlight)
      setState({
        phase,
        capability: status.capability,
        status,
        kind: phase === 'refreshing' ? state.kind : undefined,
        announcement: pollAnnouncement(status, phase, state),
      })
      // The server's refresh_due already waits out a failure's retry delay and stops for one
      // that needs the user, so a failed last attempt does not block it.
      if (
        inFlight ||
        phase === 'refreshing' ||
        phase === 'reconnect' ||
        !status.capability.available ||
        status.refresh_due === 'none'
      )
        return
      await refresh(false)
    } catch {
      setState({ ...state, phase: 'failed', announcement: 'Provider status is unavailable.' })
    }
  }

  function dismissConfirmation(): void {
    if (state.phase !== 'confirmation') return
    setState({
      ...state,
      phase: 'idle',
      announcement: 'Provider refresh confirmation cancelled.',
    })
  }

  return {
    get state() {
      subscribe()
      return state
    },
    sync,
    poll,
    refresh: () => refresh(true),
    confirm,
    dismissConfirmation,
  }
}

function failureReason(problem: Problem): string {
  return problem.code === 'internal_error' ? `${problem.detail} ${serverLogHint}` : problem.detail
}

function pollAnnouncement(
  status: ProviderStatus,
  phase: ProviderPhase,
  previous: ProviderState,
): string {
  if (phase === 'reconnect') return 'Reconnect through the command line.'
  if (phase === 'refreshing') return 'Refreshing provider data…'
  if (phase === 'failed') {
    return `Last refresh failed: ${status.code_detail ?? 'the provider reported an error.'}`
  }
  if (previous.phase === 'reconnect') return 'Provider session restored.'
  return previous.announcement
}

function statusPhase(status: ProviderStatus, inFlight: boolean): ProviderPhase {
  if (status.code === 'provider_reconnect_required') return 'reconnect'
  if (status.code === 'provider_refresh_in_progress' || inFlight) return 'refreshing'
  if (status.code !== undefined) return 'failed'
  return 'idle'
}
