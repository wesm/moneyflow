import { createSubscriber } from 'svelte/reactivity'

import {
  MoneyflowProblem,
  requestProfileJSON,
  type MutationFetch,
  type ProviderConfirmationBody,
  type ProviderRefreshBody,
  type ProviderRefreshResponse,
  type ProviderStatus,
  type ViewProjection,
} from '../api/client'

type Capability = NonNullable<ViewProjection['capabilities']>[number]
export type ProviderPhase = 'idle' | 'refreshing' | 'confirmation' | 'reconnect' | 'failed'

export interface ProviderState {
  phase: ProviderPhase
  announcement: string
  capability: Capability | undefined
  status?: ProviderStatus
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
}

interface ProviderControllerOptions {
  transport: ProviderTransport
  host: ProviderProjectionHost
  now?: () => number
}

const refreshIntervalMillis = 6 * 60 * 60 * 1000

export function createProviderController(options: ProviderControllerOptions): ProviderController {
  const now = options.now ?? Date.now
  let state: ProviderState = { phase: 'idle', announcement: '', capability: undefined }
  let inFlight = false
  let notify = (): void => undefined
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => {
      notify = () => undefined
    }
  })

  function setState(next: ProviderState): void {
    state = next
    notify()
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
    setState({ ...state, phase: 'refreshing', announcement: 'Refreshing provider data…' })
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
    try {
      const response = await requestProfileJSON<ProviderRefreshResponse>(
        options.transport.mutations,
        path,
        body,
      )
      if (response.projection.canonical_query !== current.canonical_query) {
        throw new Error('The provider refresh changed the analytical view.')
      }
      const projection = {
        ...response.projection,
        selection: response.selection.value,
      } as ViewProjection
      options.host.accept(projection)
      setState({
        phase: 'idle',
        capability: state.capability,
        status: response.status,
        announcement:
          response.selection.kind === 'cleared'
            ? 'Provider refresh complete. Selection cleared because selected rows changed.'
            : 'Provider refresh complete.',
      })
      return true
    } catch (error) {
      if (error instanceof MoneyflowProblem) {
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
        if (problem.code === 'provider_reconnect_required') {
          setState({
            ...state,
            phase: 'reconnect',
            announcement: problem.detail,
          })
          return false
        }
        if (confirmation && problem.code === 'provider_confirmation_invalid') {
          await refreshStatusAfterInvalidConfirmation(problem.detail)
          return false
        }
        setState({ ...state, phase: 'failed', announcement: problem.detail })
        return false
      }
      setState({
        ...state,
        phase: 'failed',
        announcement: 'Provider refresh failed. Press r to try again.',
      })
      return false
    }
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
    try {
      const status = await options.transport.status()
      const wasReconnect = state.phase === 'reconnect'
      const phase = statusPhase(status, inFlight)
      setState({
        phase,
        capability: status.capability,
        status,
        announcement:
          phase === 'reconnect'
            ? 'Reconnect through the command line.'
            : phase === 'refreshing'
              ? 'Refreshing provider data…'
              : wasReconnect
                ? 'Provider session restored.'
                : state.announcement,
      })
      if (
        inFlight ||
        status.code !== undefined ||
        phase !== 'idle' ||
        !status.capability.available ||
        !isRefreshDue(status, now())
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

function isRefreshDue(status: ProviderStatus, now: number): boolean {
  if (!status.last_success) return true
  const lastSuccess = Date.parse(status.last_success)
  return Number.isFinite(lastSuccess) && now - lastSuccess >= refreshIntervalMillis
}

function statusPhase(status: ProviderStatus, inFlight: boolean): ProviderPhase {
  if (status.code === 'provider_reconnect_required') return 'reconnect'
  if (status.code === 'provider_refresh_in_progress' || inFlight) return 'refreshing'
  if (status.code !== undefined) return 'failed'
  return 'idle'
}
