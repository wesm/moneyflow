import { createSubscriber } from 'svelte/reactivity'

import {
  MoneyflowProblem,
  requestProfileJSON,
  type MutationFetch,
  type ProviderWriteConfirmationBody,
  type ProviderWriteControlBody,
  type ProviderWriteReconcileBody,
  type ProviderWriteResponse,
  type ProviderWriteStatus,
  type ViewProjection,
} from '../api/client'
import { providerName } from '../provider-name'
import { problemReason, showFailure, showWarning } from '../shell/notices'

export type ProviderWritePhase =
  | 'idle'
  | 'active'
  | 'paused'
  | 'attention'
  | 'confirmation'
  | 'reconnect'
  | 'complete'
  | 'failed'

export interface ProviderWriteState {
  phase: ProviderWritePhase
  announcement: string
  status?: ProviderWriteStatus
}

export interface ProviderWriteController {
  readonly state: ProviderWriteState
  install(status: ProviderWriteStatus): void
  poll(): Promise<void>
  pause(): Promise<boolean>
  resume(): Promise<boolean>
  reconcile(): Promise<boolean>
  confirm(): Promise<boolean>
  can(action: string): boolean
}

export interface ProviderWriteTransport {
  mutations: MutationFetch
  status(signal?: AbortSignal): Promise<ProviderWriteStatus>
}

interface ProviderWriteHost {
  current(): ViewProjection | undefined
  /**
   * Marks the view a write request starts from. The returned function shows the response's view
   * only while no view change has started since, and says whether it did.
   */
  claim(): (projection: ViewProjection) => boolean
  /** Rereads and shows the view at the newest revision; throws when the reread fails. */
  reload(): Promise<unknown>
}

interface ProviderWriteControllerOptions {
  transport: ProviderWriteTransport
  host: ProviderWriteHost
  visible?: () => boolean
  now?: () => number
}

export function createProviderWriteController(
  options: ProviderWriteControllerOptions,
): ProviderWriteController {
  const visible = options.visible ?? (() => globalThis.document?.visibilityState !== 'hidden')
  const now = options.now ?? Date.now
  let state: ProviderWriteState = { phase: 'idle', announcement: '' }
  let confirmationToken = ''
  let inFlight = false
  // The run failure and status failure already shown, so a poll each second repeats neither.
  let shownRunFailure = ''
  let statusFailing = false
  let notify = (): void => undefined
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => {
      notify = () => undefined
    }
  })

  function setState(next: ProviderWriteState): void {
    state = next
    notify()
  }

  function install(status: ProviderWriteStatus): void {
    if (repeatsFinishedResult(status)) {
      setState({ ...state, status })
      return
    }
    const retainConfirmation =
      confirmationToken !== '' &&
      state.status?.phase === 'reconcile_confirmation_required' &&
      status.phase === 'reconcile_confirmation_required' &&
      state.status.batch_version === status.batch_version
    if (!retainConfirmation) confirmationToken = ''
    const stopped = runFailureMessage(status)
    setState({
      status,
      phase: phaseFor(status),
      announcement:
        stopped ?? announcementFor(status, providerName(options.host.current()?.profile_kind)),
    })
    if (stopped && stopped !== shownRunFailure) showFailure(stopped)
    shownRunFailure = stopped ?? ''
  }

  // Polling an unchanged finished result must keep the announcement that completion chose.
  function repeatsFinishedResult(status: ProviderWriteStatus): boolean {
    const previous = state.status
    return (
      state.phase === 'complete' &&
      !status.phase &&
      !previous?.phase &&
      status.revision === previous?.revision &&
      status.completed_at === previous?.completed_at &&
      status.audit_warning === previous?.audit_warning
    )
  }

  function can(action: string): boolean {
    subscribe()
    if (state.status?.phase === 'reconcile_confirmation_required') {
      if (action === 'confirm') return confirmationToken !== ''
      if (action === 'reconcile') return confirmationToken === ''
    }
    return (state.status?.actions ?? []).includes(action)
  }

  async function poll(): Promise<void> {
    if (!visible() || inFlight) return
    try {
      const previous = state.status
      const status = await options.transport.status()
      statusFailing = false
      install(status)
      if (!status.phase) {
        if (previous?.phase) await complete()
        return
      }
      if (status.phase === 'writing' || status.phase === 'reconciling') {
        if (can('resume')) await resume()
        else if (
          status.phase === 'reconciling' &&
          can('reconcile') &&
          options.host.current()?.profile_kind !== 'monarch'
        )
          await reconcile()
        return
      }
      if (
        status.phase === 'rate_limited' &&
        can('resume') &&
        (!status.next_eligible || Date.parse(status.next_eligible) <= now())
      ) {
        await resume()
        return
      }
      if (status.phase === 'reconnect_required') {
        if (can('resume')) await resume()
        else if (
          can('reconcile') &&
          !(status.actions ?? []).includes('reconnect') &&
          options.host.current()?.profile_kind !== 'monarch'
        )
          await reconcile()
      }
    } catch {
      const announcement =
        'Provider write status is unavailable, so the progress shown may be out of date.'
      setState({ ...state, phase: 'failed', announcement })
      if (!statusFailing) showFailure(announcement)
      statusFailing = true
    }
  }

  async function pause(): Promise<boolean> {
    return control('api/v1/provider/write/pause')
  }

  async function resume(): Promise<boolean> {
    return control('api/v1/provider/write/resume')
  }

  async function control(path: string): Promise<boolean> {
    const version = state.status?.batch_version
    if (!version || inFlight) return false
    const body: ProviderWriteControlBody = {
      version: '1',
      expected_batch_version: version,
    }
    return submit(path, body)
  }

  async function reconcile(): Promise<boolean> {
    const body = reconcileBody()
    if (!body || inFlight) return false
    return submit('api/v1/provider/write/reconcile', body)
  }

  async function confirm(): Promise<boolean> {
    const body = reconcileBody()
    if (!body || !confirmationToken || inFlight) return false
    const confirmed: ProviderWriteConfirmationBody = {
      ...body,
      confirmation_token: confirmationToken,
    }
    return submit('api/v1/provider/write/reconcile/confirm', confirmed, true)
  }

  function reconcileBody(): ProviderWriteReconcileBody | undefined {
    const current = options.host.current()
    const version = state.status?.batch_version
    if (!current || !version) return undefined
    return {
      version: '1',
      expected_batch_version: version,
      query: current.canonical_query,
      selection: current.selection,
      window: { offset: current.window.offset, limit: current.window.limit },
    }
  }

  async function submit(
    path: string,
    body: ProviderWriteControlBody | ProviderWriteReconcileBody | ProviderWriteConfirmationBody,
    confirming = false,
  ): Promise<boolean> {
    inFlight = true
    const show = options.host.claim()
    try {
      const response = await requestProfileJSON<ProviderWriteResponse>(
        options.transport.mutations,
        path,
        body,
      )
      install(response.status)
      if (response.projection) {
        const current = options.host.current()
        const shown =
          (!current || response.projection.canonical_query === current.canonical_query) &&
          show({
            ...response.projection,
            selection: response.selection?.value ?? response.projection.selection,
          } as ViewProjection)
        // The view moved on while the server worked; read it again instead of replacing it.
        if (!shown && response.status.phase) await refreshView()
      }
      if (!response.status.phase) await complete(path.includes('/reconcile'))
      return true
    } catch (error) {
      if (error instanceof MoneyflowProblem) {
        const problem = error.problem
        if (
          problem.code === 'provider_deletion_confirmation_required' &&
          problem.provider_write &&
          problem.provider_write_confirmation_token
        ) {
          confirmationToken = problem.provider_write_confirmation_token
          setState({
            phase: 'confirmation',
            status: problem.provider_write,
            announcement: problem.detail,
          })
          return false
        }
        if (confirming && problem.code === 'provider_confirmation_invalid') {
          confirmationToken = ''
          setState({ ...state, phase: 'failed', announcement: problem.detail })
          return false
        }
        if (problem.provider_write) install(problem.provider_write)
        setState({ ...state, phase: phaseForProblem(problem.code), announcement: problem.detail })
        return false
      }
      setState({ ...state, phase: 'failed', announcement: 'The provider write request failed.' })
      return false
    } finally {
      inFlight = false
    }
  }

  async function complete(reconciled = false): Promise<void> {
    const refreshed = await refreshView()
    const finished = finishedMessage(state.status, reconciled)
    setState({
      ...(state.status ? { status: state.status } : {}),
      phase: 'complete',
      announcement: [finished, refreshed ? '' : viewNotRefreshed, state.status?.audit_warning]
        .filter(Boolean)
        .join(' '),
    })
    // The write finished; only the view on screen is old.
    if (!refreshed) showWarning(`${finished} ${viewNotRefreshed}`)
  }

  /** Rereads the view at the newest revision; false when that request fails. */
  async function refreshView(): Promise<boolean> {
    try {
      await options.host.reload()
      return true
    } catch {
      return false
    }
  }

  return {
    get state() {
      subscribe()
      return state
    },
    install,
    poll,
    pause,
    resume,
    reconcile,
    confirm,
    can,
  }
}

const viewNotRefreshed = 'The view could not be refreshed; reload the page to see it.'

/** Why a background run stopped while its batch still reads as active, if it did. */
function runFailureMessage(status: ProviderWriteStatus): string | undefined {
  const failure = status.run_failure
  if (!failure || (status.phase !== 'writing' && status.phase !== 'reconciling')) return undefined
  return `Provider write stopped: ${problemReason(failure)} Moneyflow resumes it within 2 minutes.`
}

function finishedMessage(status: ProviderWriteStatus | undefined, reconciled: boolean): string {
  if (status?.completed_at) return 'Provider write complete.'
  return reconciled ? 'Provider write stopped. Remaining edits discarded.' : 'Provider write ended.'
}

function phaseFor(status: ProviderWriteStatus): ProviderWritePhase {
  if (!status.phase) return status.completed_at || status.audit_warning ? 'complete' : 'idle'
  if (status.phase === 'paused') return 'paused'
  if (status.phase === 'attention_required') return 'attention'
  if (status.phase === 'reconcile_confirmation_required') return 'confirmation'
  if (status.phase === 'reconnect_required') return 'reconnect'
  return 'active'
}

function phaseForProblem(code: string): ProviderWritePhase {
  if (code === 'provider_reconnect_required') return 'reconnect'
  if (code === 'provider_write_paused') return 'paused'
  if (code === 'provider_write_attention_required') return 'attention'
  return 'failed'
}

function announcementFor(status: ProviderWriteStatus, providerName: string): string {
  if (!status.phase)
    return [
      status.completed_at ? 'Provider write complete.' : 'No provider write is active.',
      status.audit_warning,
    ]
      .filter(Boolean)
      .join(' ')
  if (status.phase === 'paused')
    return 'Provider write paused. Already accepted writes remain applied.'
  if (status.phase === 'attention_required') {
    if (
      status.reason === 'provider_write_outcome_unknown' &&
      (status.actions ?? []).includes('resume')
    )
      return 'A provider request may have succeeded. Check and resume verifies affected transactions before continuing.'
    return attentionMessage(status.reason, providerName)
  }
  if (status.phase === 'reconnect_required')
    return `Reconnect ${providerName} to continue this write.`
  if (status.phase === 'rate_limited')
    return `${providerName} rate limited this write; it will resume when eligible.`
  if (status.phase === 'reconcile_confirmation_required') {
    return 'Confirm the provider reconciliation before removing local rows.'
  }
  return `Writing pending changes to ${providerName}.`
}

function attentionMessage(reason: string | undefined, providerName: string): string {
  if (reason === 'provider_write_target_not_found') {
    return 'A provider target is no longer available. Stop and reconcile provider truth.'
  }
  if (reason === 'provider_write_outcome_unknown') {
    return 'A provider request may have succeeded. Stop and reconcile before continuing.'
  }
  if (reason === 'provider_write_unavailable_exhausted') {
    return `${providerName} remained unavailable. Resume to retry or stop and reconcile.`
  }
  return 'The provider write requires attention.'
}
