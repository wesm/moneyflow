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
import { failureReason, problemReason, showSuccess, showWarning } from '../shell/notices'
import { providerLabel, refreshOwner } from './refresh-notice'

type Capability = NonNullable<ViewProjection['capabilities']>[number]
export type ProviderPhase = 'idle' | 'refreshing' | 'confirmation' | 'reconnect' | 'failed'
/** What a running Monarch refresh reads: the last 90 days, or complete history. */
export type RefreshKind = 'recent' | 'full'

/**
 * What the refresh dialog shows: this tab's refresh running or stopping, another interface's
 * refresh, a failure, a reconnect request, or removals to confirm.
 */
export type RefreshDialogView =
  | 'progress'
  | 'stopping'
  | 'other'
  | 'failed'
  | 'reconnect'
  | 'confirmation'

export interface RefreshDialog {
  view: RefreshDialogView
  /** When this tab saw the refresh start, for the elapsed time. */
  startedAt: number
  /** Why the refresh failed, in the failed and reconnect views. */
  reason?: string | undefined
}

export interface ProviderState {
  phase: ProviderPhase
  announcement: string
  capability: Capability | undefined
  status?: ProviderStatus
  kind?: RefreshKind | undefined
  /** The open refresh dialog; undefined while it is closed. */
  dialog?: RefreshDialog | undefined
  /** Why the shown data missed a refresh, when this tab knows more than the status says. */
  staleReason?: string | undefined
  /** Why the last status request failed; the next answer clears it. */
  statusError?: string | undefined
}

export interface ProviderController {
  readonly state: ProviderState
  sync(projection: ViewProjection): void
  poll(): Promise<void>
  refresh(): Promise<boolean>
  confirm(): Promise<boolean>
  dismissConfirmation(): void
  /** Stops this tab's refresh; the dialog says Stopping… until the server has stopped it. */
  cancel(): void
  /**
   * Closes a failed refresh's dialog, or one that cannot follow a refresh because status checks
   * fail, and keeps the data shown under the stale banner.
   */
  continueStale(): void
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
  now?: () => number
}

/** Status polling interval while a refresh runs, so progress and the outcome appear promptly. */
const refreshingPollMillis = 1_000
/**
 * How long a canceled refresh holds off automatic refresh. It matches the server's retry delay
 * after a stopped refresh, which the server cannot apply to a cancel that arrived before it took
 * the refresh.
 */
const canceledHoldMillis = 5 * 60_000
export const REFRESH_CANCELED = 'Refresh was canceled; no provider data changed.'
export const REFRESH_STOPPED = 'Refresh stopped. The view shows the latest saved data.'
export const REFRESH_NOT_CONFIRMED = 'Refresh was not confirmed; no provider data changed.'

export function createProviderController(options: ProviderControllerOptions): ProviderController {
  const now = options.now ?? Date.now
  let state: ProviderState = { phase: 'idle', announcement: '', capability: undefined }
  let inFlight = false
  /** A refresh asked for before the first view loaded; it starts with that view. */
  let pending: boolean | undefined
  let request: AbortController | undefined
  /** The data generation before the watched refresh; a newer one means a refresh committed. */
  let watched: string | undefined
  let settled = 0
  /** Automatic refresh waits until this time after the user canceled one. */
  let automaticAfter = 0
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
    const watching = state.phase === 'refreshing' || state.dialog?.view === 'stopping'
    if (watching && refreshingPoll === undefined) {
      refreshingPoll = setInterval(() => void poll(), refreshingPollMillis)
    } else if (!watching && refreshingPoll !== undefined) {
      clearInterval(refreshingPoll)
      refreshingPoll = undefined
    }
    notify()
  }

  /**
   * Records the data generation before this attempt from the newest status the server sent. A
   * status that shows a refresh running leaves it unknown until a poll reads it.
   */
  function startAttempt(): void {
    watched = state.status?.refreshing ? undefined : state.status?.generation
  }

  function openDialog(view: RefreshDialogView, reason?: string): RefreshDialog {
    return { view, startedAt: state.dialog?.startedAt ?? now(), reason }
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
    if (pending === undefined || inFlight) return
    const manual = pending
    pending = undefined
    void send(manual, projection)
  }

  async function refresh(manual = true): Promise<boolean> {
    const capability = state.capability
    if (!capability?.available) {
      setState({
        ...state,
        announcement: capability?.reason ?? 'Provider refresh is unavailable.',
      })
      return false
    }
    if (inFlight || pending !== undefined) return false
    if (manual) automaticAfter = 0
    startAttempt()
    setState({
      ...state,
      phase: 'refreshing',
      kind: refreshKind(manual),
      staleReason: undefined,
      dialog: { ...openDialog('progress'), startedAt: now() },
      announcement: `Refreshing from ${providerLabel(state.status?.provider_kind)}…`,
    })
    const current = options.host.current()
    if (!current) {
      pending = manual
      return false
    }
    return send(manual, current)
  }

  async function send(manual: boolean, current: ViewProjection): Promise<boolean> {
    const body: ProviderRefreshBody = {
      version: '1',
      manual,
      query: current.canonical_query,
      selection: current.selection,
      window: { offset: current.window.offset, limit: current.window.limit },
    }
    return submit('api/v1/provider/refresh', body)
  }

  async function confirm(): Promise<boolean> {
    const current = options.host.current()
    const token = state.status?.confirmation_token
    if (!current || state.phase !== 'confirmation' || !token || inFlight) return false
    startAttempt()
    setState({
      ...state,
      phase: 'refreshing',
      dialog: openDialog('progress'),
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
    return submit('api/v1/provider/refresh/confirm', body, true)
  }

  async function submit(
    path: string,
    body: ProviderRefreshBody | ProviderConfirmationBody,
    confirmation = false,
  ): Promise<boolean> {
    const current = options.host.current()
    if (!current) return false
    inFlight = true
    request = new AbortController()
    const signal = request.signal
    let response: ProviderRefreshResponse
    try {
      response = await requestProfileJSON<ProviderRefreshResponse>(
        options.transport.mutations,
        path,
        body,
        signal,
      )
    } catch (error) {
      settled++
      if (signal.aborted) return stopping()
      return fail(error, confirmation)
    } finally {
      inFlight = false
      request = undefined
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
    succeed(response.status, complete)
    return true
  }

  function succeed(status: ProviderStatus, viewNote = ''): void {
    const summary = completeMessage(status)
    showSuccess(summary)
    if (status.warning) showWarning(status.warning)
    setState({
      phase: 'idle',
      capability: state.capability,
      status,
      announcement: [summary, viewNote, status.warning].filter(Boolean).join(' '),
    })
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
      return selectionCleared ? 'Selection cleared because selected rows changed.' : ''
    }
    return reread()
  }

  async function reread(): Promise<string> {
    try {
      await options.host.reload()
      return ''
    } catch {
      return 'The current view could not be reread.'
    }
  }

  /** The request ended because the user canceled it; status polls follow the server's stop. */
  function stopping(): boolean {
    void poll()
    return false
  }

  async function fail(error: unknown, confirmation: boolean): Promise<boolean> {
    if (!(error instanceof MoneyflowProblem)) {
      return failed(failureReason(error))
    }
    const problem = error.problem
    const status = problem.provider === undefined ? {} : { status: problem.provider }
    if (problem.code === 'provider_deletion_confirmation_required' && problem.provider) {
      setState({
        ...state,
        ...status,
        phase: 'confirmation',
        dialog: openDialog('confirmation'),
        announcement: problem.detail,
      })
      return false
    }
    if (problem.code === 'provider_refresh_in_progress') {
      // The server read this status before the other refresh could commit.
      watched = problem.provider?.generation
      setState({ ...state, ...status, phase: 'refreshing', dialog: openDialog('other') })
      return false
    }
    if (problem.code === 'provider_reconnect_required') return needsReconnect(problem.provider)
    if (confirmation && problem.code === 'provider_confirmation_invalid') {
      await refreshStatusAfterInvalidConfirmation(problem.detail)
      return false
    }
    setState({ ...state, ...status })
    return failed(problemReason(problem))
  }

  /** The banner offers Reconnect from the status code, so the status must carry it. */
  function needsReconnect(sent: ProviderStatus | undefined): boolean {
    const status =
      sent ?? (state.status && { ...state.status, code: 'provider_reconnect_required' })
    const reason = `${providerLabel(status?.provider_kind)} needs you to reconnect.`
    setState({
      ...state,
      ...(status === undefined ? {} : { status }),
      phase: 'reconnect',
      kind: undefined,
      dialog: openDialog('reconnect', reason),
      announcement: reason,
    })
    return false
  }

  function failed(reason: string): boolean {
    setState({
      ...state,
      phase: 'failed',
      kind: undefined,
      dialog: openDialog('failed', reason),
      announcement: `Refresh failed. ${reason}`,
    })
    return false
  }

  async function refreshStatusAfterInvalidConfirmation(detail: string): Promise<void> {
    const reason = `${detail} Retry to fetch a new candidate.`
    try {
      const status = await options.transport.status()
      setState({ ...state, capability: status.capability, status })
    } catch {
      // The failure below still explains what to do.
    }
    failed(reason)
  }

  async function poll(): Promise<void> {
    if (state.phase === 'confirmation') return
    const seen = settled
    let status: ProviderStatus
    try {
      status = await options.transport.status()
    } catch (error) {
      setState({ ...state, statusError: failureReason(error) })
      return
    }
    // A refresh that finished meanwhile returned newer status than this poll read.
    if (seen !== settled) return
    observe(status)
    // The server's refresh_due already waits out a failure's retry delay and stops for one
    // that needs the user, so a failed last attempt does not block it.
    if (
      inFlight ||
      state.dialog ||
      state.phase === 'reconnect' ||
      !status.capability.available ||
      status.refresh_due === 'none' ||
      now() < automaticAfter
    )
      return
    await refresh(false)
  }

  /** Applies one status answer: progress, another interface's refresh, or a refresh that ended. */
  function observe(status: ProviderStatus): void {
    const dialog = state.dialog
    // A running refresh has not committed yet, so its generation is the one before it.
    if (status.refreshing && watched === undefined) watched = status.generation
    const base = { ...state, capability: status.capability, status, statusError: undefined }
    if (dialog?.view === 'stopping' || dialog?.view === 'other') {
      if (status.refreshing) return setState({ ...base, phase: 'refreshing' })
      return finishWatched(status, dialog.view === 'stopping')
    }
    if (status.refreshing && !inFlight && pending === undefined && !dialog) {
      const opened = openDialog('other')
      watched = status.generation
      setState({
        ...base,
        phase: 'refreshing',
        kind: undefined,
        dialog: opened,
        announcement: `${refreshOwner(status.owner_renderer)} is refreshing this profile.`,
      })
      return
    }
    const phase = statusPhase(status, inFlight || pending !== undefined)
    const elsewhere = refreshedElsewhere(status)
    setState({
      ...base,
      phase,
      kind: phase === 'refreshing' ? state.kind : undefined,
      announcement: pollAnnouncement(status, phase, state),
      ...(elsewhere ? { dialog: undefined, staleReason: undefined } : {}),
    })
    if (elsewhere) void rereadAnnouncingFailure()
  }

  async function rereadAnnouncingFailure(): Promise<void> {
    const note = await reread()
    if (note) setState({ ...state, announcement: note })
  }

  /**
   * Another interface committed newer data while this tab had no refresh of its own running, so
   * a failure this tab shows is obsolete and the view predates that data.
   */
  function refreshedElsewhere(status: ProviderStatus): boolean {
    const known = state.status?.generation
    const view = state.dialog?.view
    return (
      known !== undefined &&
      status.generation !== known &&
      !status.refreshing &&
      !inFlight &&
      pending === undefined &&
      (view === undefined || view === 'failed' || view === 'reconnect')
    )
  }

  /** A refresh this tab watched has ended: it committed newer data, or it stopped. */
  function finishWatched(status: ProviderStatus, canceled: boolean): void {
    if (watched !== undefined && status.generation !== watched) {
      setState({ ...state, status, dialog: undefined })
      void reread().then((note) => succeed(status, note))
      return
    }
    const ended = {
      ...state,
      phase: statusPhase(status, false),
      status,
      capability: status.capability,
      kind: undefined,
      dialog: undefined,
    }
    // Without the generation from before the refresh, it may have saved before it stopped, and
    // the counts in the status may be an earlier refresh's, so the view is reread and neither a
    // success nor an unchanged result is claimed.
    if (watched === undefined) {
      setState({ ...ended, announcement: canceled ? REFRESH_STOPPED : 'The other refresh ended.' })
      void rereadAnnouncingFailure()
      return
    }
    setState({
      ...ended,
      staleReason: canceled ? REFRESH_CANCELED : state.staleReason,
      announcement: canceled ? REFRESH_CANCELED : 'The other refresh ended.',
    })
  }

  function cancel(): void {
    if (state.dialog?.view !== 'progress') return
    automaticAfter = now() + canceledHoldMillis
    if (pending !== undefined) {
      pending = undefined
      setState({
        ...state,
        phase: 'idle',
        kind: undefined,
        dialog: undefined,
        staleReason: REFRESH_CANCELED,
        announcement: REFRESH_CANCELED,
      })
      return
    }
    if (!request) return
    setState({
      ...state,
      dialog: { ...state.dialog, view: 'stopping' },
      announcement: 'Stopping the refresh…',
    })
    request.abort()
  }

  function continueStale(): void {
    const dialog = state.dialog
    if (dialog?.view === 'failed' || dialog?.view === 'reconnect') {
      setState({ ...state, dialog: undefined, staleReason: dialog.reason })
      return
    }
    // Without status answers the dialog cannot tell when the refresh ends, so the user may leave
    // it; the stale banner then reports the failed check and offers Retry.
    if ((dialog?.view === 'stopping' || dialog?.view === 'other') && state.statusError) {
      setState({
        ...state,
        phase: 'idle',
        kind: undefined,
        dialog: undefined,
        announcement: state.statusError,
      })
    }
  }

  function dismissConfirmation(): void {
    if (state.phase !== 'confirmation') return
    setState({
      ...state,
      phase: 'idle',
      dialog: undefined,
      staleReason: REFRESH_NOT_CONFIRMED,
      announcement: REFRESH_NOT_CONFIRMED,
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
    cancel,
    continueStale,
  }
}

/** Such as `Refresh complete: 40 imported, 0 removed.` */
export function completeMessage(status: ProviderStatus): string {
  const { imported_transactions, removed_transactions } = status.summary
  return `Refresh complete: ${imported_transactions} imported, ${removed_transactions} removed.`
}

function pollAnnouncement(
  status: ProviderStatus,
  phase: ProviderPhase,
  previous: ProviderState,
): string {
  if (phase === 'reconnect') return 'Reconnect through the command line.'
  if (phase === 'refreshing') return previous.announcement
  if (phase === 'failed') {
    return `Last refresh failed: ${status.code_detail ?? 'the provider reported an error.'}`
  }
  if (previous.phase === 'reconnect') return 'Provider session restored.'
  return previous.announcement
}

function statusPhase(status: ProviderStatus, inFlight: boolean): ProviderPhase {
  if (status.code === 'provider_reconnect_required') return 'reconnect'
  if (status.refreshing || inFlight) return 'refreshing'
  if (status.code !== undefined && status.code !== 'provider_refresh_stale') return 'failed'
  return 'idle'
}
