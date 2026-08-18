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
  accept(projection: ViewProjection): void
  reload(): Promise<ViewProjection | undefined>
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
    confirmationToken = ''
    setState({
      status,
      phase: phaseFor(status),
      announcement: announcementFor(status),
    })
  }

  function can(action: string): boolean {
    return (state.status?.actions ?? []).includes(action)
  }

  async function poll(): Promise<void> {
    if (!visible() || inFlight) return
    try {
      const previous = state.status
      const status = await options.transport.status()
      install(status)
      if (!status.phase) {
        if (previous?.phase) await complete()
        return
      }
      if (status.phase === 'writing' || status.phase === 'reconciling') {
        if (can('resume')) await resume()
        else if (status.phase === 'reconciling' && can('reconcile')) await reconcile()
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
        else if (can('reconcile') && !(status.actions ?? []).includes('reconnect'))
          await reconcile()
      }
    } catch {
      setState({ ...state, phase: 'failed', announcement: 'Provider write status is unavailable.' })
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
    try {
      const response = await requestProfileJSON<ProviderWriteResponse>(
        options.transport.mutations,
        path,
        body,
      )
      install(response.status)
      if (response.projection) {
        const current = options.host.current()
        if (current && response.projection.canonical_query !== current.canonical_query) {
          throw new Error('Provider reconciliation changed analytical browser state.')
        }
        options.host.accept({
          ...response.projection,
          selection: response.selection?.value ?? response.projection.selection,
        } as ViewProjection)
      }
      if (!response.status.phase) await complete()
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

  async function complete(): Promise<void> {
    const projection = await options.host.reload()
    if (projection) options.host.accept(projection)
    setState({
      ...(state.status ? { status: state.status } : {}),
      phase: 'complete',
      announcement: 'Provider write complete. Provider refresh is due.',
    })
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

function phaseFor(status: ProviderWriteStatus): ProviderWritePhase {
  if (!status.phase) return 'idle'
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

function announcementFor(status: ProviderWriteStatus): string {
  if (!status.phase) return 'No provider write is active.'
  if (status.phase === 'paused')
    return 'Provider write paused. Already accepted writes remain applied.'
  if (status.phase === 'attention_required') return attentionMessage(status.reason)
  if (status.phase === 'reconnect_required') return 'Reconnect Monarch to continue this write.'
  if (status.phase === 'rate_limited')
    return 'Monarch rate limited this write; it will resume when eligible.'
  if (status.phase === 'reconcile_confirmation_required') {
    return 'Confirm the provider reconciliation before removing local rows.'
  }
  return 'Writing pending changes to Monarch.'
}

function attentionMessage(reason?: string): string {
  if (reason === 'provider_write_target_not_found') {
    return 'A provider target is no longer available. Stop and reconcile provider truth.'
  }
  if (reason === 'provider_write_outcome_unknown') {
    return 'A provider request may have succeeded. Stop and reconcile before continuing.'
  }
  if (reason === 'provider_write_unavailable_exhausted') {
    return 'Monarch remained unavailable. Resume to retry or stop and reconcile.'
  }
  return 'The provider write requires attention.'
}
