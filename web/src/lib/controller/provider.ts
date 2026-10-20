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
import { failureReason, problemReason, showNotice } from '../shell/notices'
import { auditWarningCode } from './editing'
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
  'progress' | 'stopping' | 'other' | 'failed' | 'reconnect' | 'confirmation'

export interface RefreshDialog {
  view: RefreshDialogView
  /** When this tab saw the refresh start, for the elapsed time. */
  startedAt: number
  /** Why the refresh failed, in the failed and reconnect views. */
  reason?: string | undefined
  /** The user asked this refresh for complete history, so Retry asks again. */
  completeHistory?: boolean | undefined
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
  /** Why the view could not load the newest saved provider data; loading it clears this. */
  viewError?: string | undefined
  /** The view is being reread because it predates the newest saved provider data. */
  viewLoading?: boolean | undefined
}

export interface ProviderController {
  readonly state: ProviderState
  /**
   * Whether Cancel can stop this tab's refresh, as in standalone moneyflow. A host's server runs
   * a refresh on after its request ends, so in a host the dialog offers Close instead.
   */
  readonly cancelable: boolean
  sync(projection: ViewProjection): void
  poll(): Promise<void>
  /**
   * Starts a manual refresh. A Monarch refresh reads the last 90 days unless `completeHistory`
   * asks for complete history; the server reads complete history anyway when it is due.
   */
  refresh(completeHistory?: boolean): Promise<boolean>
  confirm(): Promise<boolean>
  dismissConfirmation(): void
  /** Rereads the view after newer provider data could not be loaded. */
  reread(): Promise<void>
  /** Stops this tab's refresh; the dialog says Stopping… until the server has stopped it. */
  cancel(): void
  /** Closes this tab's progress dialog; the refresh runs on, and the status strip shows it. */
  hide(): void
  /**
   * Closes a failed refresh's dialog, or one that cannot follow a refresh because status checks
   * fail, and keeps the data shown under the stale banner.
   */
  continueStale(): void
  /**
   * Stops polling status for good, as when the workspace that showed this profile is gone. A refresh
   * or status answer still on its way then shows nothing: no view, no reread and no notice.
   */
  stop(): void
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
  /** Whether a poll starts a refresh that is due. Default true. */
  autoRefresh?: boolean
  /**
   * Standalone moneyflow, whose server ends a refresh when its request ends: Cancel stops this
   * tab's refresh, and another interface's refresh shows in a dialog. Default false.
   */
  standalone?: boolean
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
export const REFRESH_STOPPED = 'Refresh stopped.'
export const REFRESH_NOT_CONFIRMED = 'Refresh was not confirmed; no provider data changed.'
const REREAD_FAILED = 'The current view could not be reread.'

/** One refresh this tab asks for: manual or automatic, and whether it asks for complete history. */
interface RefreshRequest {
  manual: boolean
  completeHistory: boolean
}

export function createProviderController(options: ProviderControllerOptions): ProviderController {
  const now = options.now ?? Date.now
  /**
   * In a host, the server runs a refresh on after its request ends, so Cancel could not stop it,
   * and the refresh dialog offers Close. Another interface's refresh shows in the status strip,
   * not in a dialog over the host page.
   */
  const hosted = options.standalone !== true
  const autoRefresh = options.autoRefresh !== false
  let state: ProviderState = { phase: 'idle', announcement: '', capability: undefined }
  let inFlight = false
  /** A refresh asked for before the first view loaded; it starts with that view. */
  let pending: RefreshRequest | undefined
  /** What the latest refresh asked for, so Retry asks again after a host closed its dialog. */
  let latest: RefreshRequest | undefined
  let request: AbortController | undefined
  /** The data generation before the watched refresh; a newer one means a refresh committed. */
  let watched: string | undefined
  /** Counts the refreshes this tab watches, so a late reread cannot finish a newer one. */
  let attempt = 0
  let settled = 0
  let polls = 0
  /** Pending polls do not block earlier answers; only an adopted answer supersedes them. */
  let appliedPoll = 0
  /** A completion must preserve status and status-check failures observed during its reread. */
  let statusChanges = 0
  /** Automatic refresh waits until this time after the user canceled one. */
  let automaticAfter = 0
  /**
   * The newest saved provider data the server reported, with the profile revision of that report.
   * Views carry the revision they were read at, not the provider generation, so a view at this
   * revision or later shows that data.
   */
  let newest: { generation: string; revision: bigint } | undefined
  /** Counts view rereads; only the newest one's outcome changes the notice. */
  let rereads = 0
  let rereading: Promise<string> | undefined
  let refreshingPoll: ReturnType<typeof setInterval> | undefined
  let stopped = false
  let notify = (): void => undefined
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => {
      notify = () => undefined
    }
  })

  /**
   * Every status this tab adopts, from a poll, a refresh, or an error, goes through here, so no
   * path that discovers newer saved data skips rereading the view.
   */
  function setState(next: ProviderState): void {
    if (next.status && olderStatus(next.status)) {
      next = { ...next, status: state.status!, capability: state.capability }
    }
    const adopted = next.status !== undefined && next.status !== state.status
    if (adopted) statusChanges++
    if (next.status && adopted) noteNewest(next.status)
    const current = viewShowsNewest()
    state = current ? { ...next, viewError: undefined, viewLoading: undefined } : next
    const watching = !stopped && (state.phase === 'refreshing' || state.dialog?.view === 'stopping')
    if (watching && refreshingPoll === undefined) {
      refreshingPoll = setInterval(() => void poll(), refreshingPollMillis)
    } else if (!watching && refreshingPoll !== undefined) {
      clearInterval(refreshingPoll)
      refreshingPoll = undefined
    }
    notify()
    if (adopted && !current) catchUpInBackground()
  }

  /** Each status poll, and each status adopted, retries a view that is behind. */
  function catchUpInBackground(): void {
    // This tab's own refresh installs its view when it ends.
    if (inFlight || pending !== undefined || rereading !== undefined) return
    void catchUp()
  }

  function noteNewest(status: ProviderStatus): void {
    if (
      newest &&
      (BigInt(status.generation) < BigInt(newest.generation) ||
        BigInt(status.revision) < newest.revision)
    )
      return
    newest = { generation: status.generation, revision: BigInt(status.revision) }
  }

  function olderStatus(status: ProviderStatus): boolean {
    const known = state.status
    return (
      known !== undefined &&
      (BigInt(status.generation) < BigInt(known.generation) ||
        BigInt(status.revision) < BigInt(known.revision))
    )
  }

  /** Progress polls predate an attempt's result; later attempts or saved data supersede it. */
  function canAdopt(status: ProviderStatus, before: ProviderStatus | undefined): boolean {
    const known = state.status
    // SimpleFIN records admission before downloading. Other providers keep the previous
    // attempt's time while running, so matching their times does not identify the same attempt.
    const finishesAdmittedAttempt =
      known?.provider_kind === 'simplefin' &&
      status.provider_kind === 'simplefin' &&
      known.refreshing &&
      !status.refreshing &&
      status.last_attempt !== undefined &&
      status.last_attempt === known.last_attempt
    return (
      !olderStatus(status) &&
      (known === undefined ||
        known === before ||
        BigInt(status.generation) > BigInt(known.generation) ||
        BigInt(status.revision) > BigInt(known.revision) ||
        (Date.parse(status.last_attempt ?? '') || 0) >
          (Date.parse(known.last_attempt ?? '') || 0) ||
        finishesAdmittedAttempt ||
        (!known.refreshing &&
          !status.refreshing &&
          known.code === status.code &&
          known.last_attempt === status.last_attempt))
    )
  }

  /** Whether the installed view shows the newest saved provider data the server reported. */
  function viewShowsNewest(): boolean {
    if (newest === undefined) return true
    const shown = options.host.current()
    if (!shown) return true
    return BigInt(shown.revision) >= newest.revision
  }

  /**
   * Rereads the view until it shows the newest saved data. The notice says the data is loading,
   * or why it could not load, until a reread installs a view at least that new. A reread can
   * answer without installing, such as when a view change starts meanwhile; the next status poll
   * then tries again.
   */
  function catchUp(force = false): Promise<string> {
    if (!force && viewShowsNewest()) return Promise.resolve('')
    rereads++
    const work = rereadView(rereads)
    rereading = work
    void work.finally(() => {
      if (rereading === work) rereading = undefined
    })
    if (!state.viewError && !state.viewLoading) setState({ ...state, viewLoading: true })
    return work
  }

  async function rereadView(order: number): Promise<string> {
    let failure: string | undefined
    try {
      await options.host.reload()
    } catch (error) {
      failure = failureReason(error)
    }
    const note = failure === undefined ? '' : REREAD_FAILED
    // An older reread can answer last with older data, so only the newest one decides.
    if (order !== rereads) return note
    if (viewShowsNewest()) {
      setState({ ...state, viewError: undefined, viewLoading: undefined })
    } else if (failure !== undefined) {
      const viewError = `Refreshed provider data could not be loaded. ${failure}`
      setState({ ...state, viewLoading: undefined, viewError })
    }
    return note
  }

  /** A cached status can predate another interface's refresh, so it is no baseline. */
  function startAttempt(): void {
    attempt++
    watched = undefined
  }

  function openDialog(view: RefreshDialogView, reason?: string): RefreshDialog {
    return {
      view,
      startedAt: state.dialog?.startedAt ?? now(),
      reason,
      completeHistory: state.dialog?.completeHistory ?? latest?.completeHistory,
    }
  }

  /** Names what the refresh reads; the server makes the same choice from the same status. */
  function refreshKind({ manual, completeHistory }: RefreshRequest): RefreshKind | undefined {
    const status = state.status
    if (status?.provider_kind !== 'monarch') return undefined
    if (manual) return completeHistory ? 'full' : status.manual_refresh
    return status.refresh_due === 'none' ? undefined : status.refresh_due
  }

  function sync(projection: ViewProjection): void {
    const capability = projection.capabilities?.find(({ id }) => id === 'provider.refresh')
    setState({ ...state, capability })
    catchUpInBackground()
    if (pending === undefined || inFlight) return
    const asked = pending
    pending = undefined
    void send(asked, projection)
  }

  async function refresh(asked: RefreshRequest): Promise<boolean> {
    const capability = state.capability
    if (!capability?.available) {
      setState({
        ...state,
        announcement: capability?.reason ?? 'Provider refresh is unavailable.',
      })
      return false
    }
    if (inFlight || pending !== undefined) return false
    if (asked.manual) automaticAfter = 0
    latest = asked
    startAttempt()
    setState({
      ...state,
      phase: 'refreshing',
      kind: refreshKind(asked),
      staleReason: undefined,
      dialog: { view: 'progress', startedAt: now(), completeHistory: asked.completeHistory },
      announcement: `Refreshing from ${providerLabel(state.status?.provider_kind)}…`,
    })
    const current = options.host.current()
    if (!current) {
      pending = asked
      return false
    }
    return send(asked, current)
  }

  async function send(asked: RefreshRequest, current: ViewProjection): Promise<boolean> {
    const body: ProviderRefreshBody = {
      version: '1',
      manual: asked.manual,
      query: current.canonical_query,
      selection: current.selection,
      window: { offset: current.window.offset, limit: current.window.limit },
      ...(asked.completeHistory ? { complete_history: true } : {}),
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
    const finished = attempt
    const before = state.status
    let response: ProviderRefreshResponse
    try {
      signal.throwIfAborted()
      response = await requestProfileJSON<ProviderRefreshResponse>(
        options.transport.mutations,
        path,
        body,
        signal,
      )
      if (canAdopt(response.status, before)) setState({ ...state, status: response.status })
    } catch (error) {
      settled++
      if (signal.aborted) return stopping()
      return fail(error, confirmation, before)
    } finally {
      inFlight = false
      request = undefined
    }
    settled++
    if (response.projection.canonical_query !== current.canonical_query) {
      return fail(
        new Error('The provider refresh changed the analytical view.'),
        confirmation,
        before,
      )
    }
    const projection = {
      ...response.projection,
      selection: response.selection.value,
    } as ViewProjection
    noteNewest(response.status)
    leaveCompletedDialog()
    if (!viewShowsNewest()) setState({ ...state, viewLoading: true })
    const seen = statusChanges
    const viewNotes = await show(projection, current)
    succeed(response.status, viewNotes, finished, seen)
    return true
  }

  /** Reports the refresh in one notice, with what the view it installed explains. */
  function succeed(
    status: ProviderStatus,
    viewNotes: string[],
    finished: number,
    seen: number,
  ): void {
    if (stopped) return
    const message = [completeMessage(status), ...viewNotes, status.warning]
      .filter(Boolean)
      .join(' ')
    showNotice(message)
    if (finished !== attempt) return
    if (seen !== statusChanges || state.status !== status) {
      leaveCompletedDialog()
      return
    }
    setState({
      phase: 'idle',
      capability: state.capability,
      status,
      viewError: state.viewError,
      viewLoading: state.viewLoading,
      announcement: message,
    })
  }

  function leaveCompletedDialog(): void {
    const view = state.dialog?.view
    if (view !== 'progress' && view !== 'stopping') return
    const status = state.status
    if (!status) return
    if (status.refreshing) {
      attempt++
      watched = status.generation
    }
    const phase = statusPhase(status, false)
    setState({
      ...state,
      phase,
      kind: undefined,
      dialog: status.refreshing && !hosted ? openDialog('other') : undefined,
      announcement: pollAnnouncement(status, phase, state),
    })
  }

  // A view or selection change made while the refresh ran stays: wait for any still on its way,
  // so installing the refreshed view does not cancel it, then reread the view it produced at the
  // refreshed revision rather than reinstall the view the refresh was sent with. Returns what the
  // completion notice must repeat: why a reread failed, and the warnings of a view installed
  // here, such as why the refresh cleared the selection. Editing reports the audit warning.
  function show(projection: ViewProjection, sent: ViewProjection): Promise<string[]> {
    return options.host.serially(async () => {
      const before = options.host.current()
      const note = await showSettled(projection, sent)
      const installed = options.host.current()
      const warnings = installed === before ? [] : (installed?.warnings ?? [])
      return [
        ...warnings
          .filter((warning) => warning.code !== auditWarningCode)
          .map((warning) => warning.detail),
        note,
      ]
    })
  }

  async function showSettled(projection: ViewProjection, sent: ViewProjection): Promise<string> {
    if (stopped) return ''
    const shown = options.host.current()
    if (
      (shown && BigInt(shown.revision) > BigInt(projection.revision)) ||
      (newest && BigInt(projection.revision) < newest.revision)
    )
      return catchUp()
    if (
      !shown ||
      (shown.canonical_query === sent.canonical_query && shown.selection === sent.selection)
    ) {
      options.host.accept(projection)
      return ''
    }
    return catchUp(true)
  }

  /** The request ended because the user canceled it; status polls follow the server's stop. */
  function stopping(): boolean {
    void poll()
    return false
  }

  async function fail(
    error: unknown,
    confirmation: boolean,
    before: ProviderStatus | undefined,
  ): Promise<boolean> {
    if (stopped) return false
    if (!(error instanceof MoneyflowProblem)) {
      return failed(failureReason(error))
    }
    const problem = error.problem
    let reported = problem.provider
    const known = state.status
    if (
      problem.code === 'provider_deletion_confirmation_required' &&
      reported &&
      known &&
      reported.generation === known.generation &&
      BigInt(reported.revision) < BigInt(known.revision)
    ) {
      // Local edits preserve the candidate. Keep their newer revision, then check the attempt.
      reported = { ...reported, revision: known.revision }
    }
    if (reported && !canAdopt(reported, before)) {
      showNotice(problemReason(problem))
      leaveCompletedDialog()
      return false
    }
    const status = reported === undefined ? {} : { status: reported }
    if (problem.code === 'provider_deletion_confirmation_required' && reported) {
      setState({
        ...state,
        ...status,
        phase: 'confirmation',
        dialog: openDialog('confirmation'),
        announcement: problem.detail,
      })
      return false
    }
    if (problem.code === 'provider_refresh_in_progress') return followRunning(reported)
    if (problem.code === 'provider_reconnect_required') return needsReconnect(problem.provider)
    if (confirmation && problem.code === 'provider_confirmation_invalid') {
      await refreshStatusAfterInvalidConfirmation(problem.detail)
      return false
    }
    setState({ ...state, ...status })
    return failed(problemReason(problem))
  }

  /**
   * The server says a refresh is running: one this tab's request met or, in a host, this tab's
   * own, which runs on after its request ended. A host keeps any open dialog on its progress view;
   * standalone moneyflow shows the other refresh.
   */
  function followRunning(reported: ProviderStatus | undefined): boolean {
    // The server read this status before the other refresh could commit.
    watched = reported?.generation
    setState({
      ...state,
      ...(reported === undefined ? {} : { status: reported }),
      phase: 'refreshing',
      dialog: hosted ? state.dialog : openDialog('other'),
    })
    return false
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
    const finished = attempt
    const seen = statusChanges
    failed(reason)
    try {
      const status = await options.transport.status()
      if (stopped || finished !== attempt || seen !== statusChanges) return
      setState({ ...state, capability: status.capability, status })
    } catch {
      // The failure dialog already explains what to do.
    }
  }

  async function poll(): Promise<void> {
    if (state.phase === 'confirmation') return
    const seen = settled
    const order = ++polls
    let status: ProviderStatus
    try {
      status = await options.transport.status()
    } catch (error) {
      if (order < appliedPoll || seen !== settled) return
      appliedPoll = order
      statusChanges++
      setState({ ...state, statusError: failureReason(error) })
      return
    }
    // A refresh that finished meanwhile returned newer status than this poll read.
    if (stopped || seen !== settled || order < appliedPoll || olderStatus(status)) return
    appliedPoll = order
    observe(status)
    catchUpInBackground()
    if (holdsDueRefresh(status)) return
    await refresh({ manual: false, completeHistory: false })
  }

  /**
   * Whether a poll leaves the refresh the server reports as due. The server's refresh_due already
   * waits out a failure's retry delay and stops for one that needs the user, so a failed last
   * attempt does not block it. A refresh already running holds it too: in a host the status strip
   * follows that refresh, and a request from this tab would open the refresh dialog over the page.
   */
  function holdsDueRefresh(status: ProviderStatus): boolean {
    return (
      !autoRefresh ||
      status.refreshing ||
      inFlight ||
      state.dialog !== undefined ||
      state.phase === 'reconnect' ||
      !status.capability.available ||
      status.refresh_due === 'none' ||
      now() < automaticAfter
    )
  }

  /** Applies one status answer: progress, another interface's refresh, or a refresh that ended. */
  function observe(status: ProviderStatus): void {
    const view = state.dialog?.view
    const follows = following(view)
    // A running refresh has not committed yet, so its generation is the one before it.
    // A running lease does not identify this tab's request. Only watch a refresh it does not send.
    if (status.refreshing && watched === undefined && follows && view !== 'stopping') {
      watched = status.generation
    }
    const base = { ...state, capability: status.capability, status, statusError: undefined }
    if (follows) {
      if (status.refreshing) return setState({ ...base, phase: 'refreshing' })
      return finishWatched(status, view === 'stopping')
    }
    if (
      status.refreshing &&
      !inFlight &&
      pending === undefined &&
      (!view || view === 'failed' || view === 'reconnect')
    ) {
      return followOther(base, status)
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
  }

  /**
   * Whether status polls follow a refresh to its end: another interface's, this tab's after the
   * user stopped it, or in a host any refresh that no request of this tab waits on.
   */
  function following(view: RefreshDialogView | undefined): boolean {
    if (view === 'stopping' || view === 'other') return true
    return hosted && state.phase === 'refreshing' && !inFlight && pending === undefined
  }

  /** Another interface, or a host's scheduler, started a refresh this tab now follows. */
  function followOther(base: ProviderState, status: ProviderStatus): void {
    attempt++
    watched = status.generation
    const owner = hosted ? 'Moneyflow' : refreshOwner(status.owner_renderer)
    setState({
      ...base,
      phase: 'refreshing',
      kind: undefined,
      dialog: hosted ? undefined : { view: 'other', startedAt: now() },
      announcement: `${owner} is refreshing this profile.`,
    })
  }

  async function rereadOnRequest(): Promise<void> {
    const note = await catchUp(true)
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
      BigInt(status.generation) > BigInt(known) &&
      !status.refreshing &&
      !inFlight &&
      pending === undefined &&
      (view === undefined || view === 'failed' || view === 'reconnect')
    )
  }

  /** A refresh this tab watched has ended: it committed newer data, or it stopped. */
  function finishWatched(status: ProviderStatus, canceled: boolean): void {
    if (watched !== undefined && BigInt(status.generation) > BigInt(watched)) {
      setState({ ...state, status, dialog: undefined, staleReason: undefined })
      void finishCommitted(status, attempt)
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
    // the counts in the status may be an earlier refresh's, so neither a success nor an unchanged
    // result is claimed. Newer saved data is reread like any other.
    if (watched === undefined) {
      setState({
        ...ended,
        staleReason: canceled ? REFRESH_STOPPED : state.staleReason,
        announcement: canceled ? REFRESH_STOPPED : 'The other refresh ended.',
      })
      return
    }
    setState({
      ...ended,
      staleReason: canceled ? REFRESH_CANCELED : state.staleReason,
      announcement: canceled ? REFRESH_CANCELED : 'The other refresh ended.',
    })
  }

  /** A newer refresh may have started during the reread; it keeps its own dialog and status. */
  async function finishCommitted(status: ProviderStatus, finished: number): Promise<void> {
    const seen = statusChanges
    const note = await (rereading ?? Promise.resolve(''))
    succeed(status, [note], finished, seen)
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

  function hide(): void {
    if (state.dialog?.view !== 'progress') return
    setState({ ...state, dialog: undefined })
  }

  function stop(): void {
    stopped = true
    // Ends polling now: a stopped controller watches no refresh.
    setState(state)
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
    cancelable: !hosted,
    sync,
    poll,
    reread: rereadOnRequest,
    refresh: (completeHistory = false) => refresh({ manual: true, completeHistory }),
    confirm,
    dismissConfirmation,
    cancel,
    hide,
    continueStale,
    stop,
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
