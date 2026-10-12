import { showFlash } from '@kenn-io/kit-ui'
import { createSubscriber } from 'svelte/reactivity'

import {
  MoneyflowProblem,
  requestProfileJSON,
  type CommitBody,
  type EditorCatalog,
  type MerchantPreview,
  type MutationBody,
  type MutationFetch,
  type MutationInput,
  type MutationResponse,
  type PendingSummary,
  type Problem,
  type ProviderWriteStatus,
  type RevisionBody,
  type SelectionValue,
  type ViewProjection,
} from '../api/client'
import { LABEL_TAKEN } from '../editing/manager'
import { toastMessage, type ToastAction } from '../editing/toasts'

export type MutationPhase = 'idle' | 'submitting' | 'conflict' | 'failed'

export interface EditingState {
  revision: bigint
  phase: MutationPhase
  pending: PendingSummary
  announcement: string
  providerWrite?: ProviderWriteStatus
}

export interface MutationIntent {
  action: string
  input: MutationInput
  target?: { kind: string; identity: string }
}

export interface EditingProjectionHost {
  current(): ViewProjection | undefined
  accept(projection: ViewProjection): void
  refresh(selection?: SelectionValue, force?: boolean): Promise<ViewProjection | undefined>
}

export interface EditingController {
  readonly state: EditingState
  sync(projection: ViewProjection): void
  submit(intent: MutationIntent): Promise<boolean>
  undo(): Promise<boolean>
  redo(): Promise<boolean>
  commit(reviewedRevision: bigint): Promise<boolean>
  catalog(): Promise<EditorCatalog>
  previewMerchant(
    target: { kind: string; identity: string },
    scope: string,
    signal?: AbortSignal,
  ): Promise<MerchantPreview>
}

interface EditingControllerOptions {
  transport: MutationFetch
  host: EditingProjectionHost
  onProviderWrite?: (status: ProviderWriteStatus) => void
}

const emptyPending: PendingSummary = {
  active_operations: 0,
  inactive_operations: 0,
  affected_transactions: 0,
}

/** What an editing request that failed without a server answer reports. */
export const REQUEST_FAILED = 'The profile request failed.'

export function createEditingController(options: EditingControllerOptions): EditingController {
  const initial = options.host.current()
  let state: EditingState = {
    revision: parseRevision(initial?.revision ?? '0'),
    phase: 'idle',
    pending: initial?.pending ?? emptyPending,
    announcement: '',
  }
  let notify = (): void => undefined
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => {
      notify = () => undefined
    }
  })

  function setState(next: EditingState): void {
    state = next
    notify()
  }

  function sync(projection: ViewProjection): void {
    setState({
      ...state,
      revision: parseRevision(projection.revision),
      pending: projection.pending,
    })
  }

  async function submit(intent: MutationIntent): Promise<boolean> {
    const current = options.host.current()
    if (!current) return false
    if (!projectionRevisionIsCurrent(current)) return false
    const body: MutationBody = {
      version: '1',
      expected_revision: state.revision.toString(10),
      query: current.canonical_query,
      selection: current.selection,
      action: intent.action,
      input: intent.input,
      window: { offset: current.window.offset, limit: current.window.limit },
      ...(intent.target === undefined ? {} : { target: intent.target }),
    }
    return await mutate('api/v1/mutations', body, 'Change saved as pending.', 'mutate')
  }

  async function undo(): Promise<boolean> {
    return await moveCursor('api/v1/undo')
  }

  async function redo(): Promise<boolean> {
    return await moveCursor('api/v1/redo')
  }

  async function moveCursor(path: 'api/v1/undo' | 'api/v1/redo'): Promise<boolean> {
    const current = options.host.current()
    if (!current) return false
    if (!projectionRevisionIsCurrent(current)) return false
    const body: RevisionBody = {
      version: '1',
      expected_revision: state.revision.toString(10),
      query: current.canonical_query,
      selection: current.selection,
      window: { offset: current.window.offset, limit: current.window.limit },
    }
    const undoing = path.endsWith('/undo')
    return await mutate(
      path,
      body,
      undoing ? 'Pending change undone.' : 'Pending change redone.',
      undoing ? 'undo' : 'redo',
    )
  }

  async function commit(reviewedRevision: bigint): Promise<boolean> {
    const current = options.host.current()
    if (!current) return false
    if (!projectionRevisionIsCurrent(current)) return false
    const body: CommitBody = {
      version: '1',
      expected_revision: state.revision.toString(10),
      reviewed_revision: reviewedRevision.toString(10),
      query: current.canonical_query,
      selection: current.selection,
      window: { offset: current.window.offset, limit: current.window.limit },
    }
    return await mutate('api/v1/commit', body, 'Changes saved in Moneyflow.')
  }

  async function catalog(): Promise<EditorCatalog> {
    return await requestProfileJSON<EditorCatalog>(options.transport, 'api/v1/editor-catalog', {
      version: '1',
      expected_revision: state.revision.toString(10),
    })
  }

  async function previewMerchant(
    target: { kind: string; identity: string },
    scope: string,
    signal?: AbortSignal,
  ): Promise<MerchantPreview> {
    for (let attempt = 0; ; attempt++) {
      const current = options.host.current()
      if (!current || !projectionRevisionIsCurrent(current))
        throw new Error('The current view is unavailable.')
      const body: MutationBody = {
        version: '1',
        expected_revision: state.revision.toString(10),
        query: current.canonical_query,
        selection: current.selection,
        action: 'transaction.edit-merchant',
        target,
        input: { scope },
        window: { offset: 0, limit: 3 },
      }
      try {
        return await requestProfileJSON<MerchantPreview>(
          options.transport,
          'api/v1/merchant-preview',
          body,
          signal,
        )
      } catch (error) {
        if (
          attempt !== 0 ||
          !(error instanceof MoneyflowProblem) ||
          !['selection_stale', 'revision_conflict'].includes(error.problem.code)
        )
          throw error
        await handleFailure(error)
        if (state.phase === 'failed') throw error
      }
    }
  }

  async function mutate(
    path: string,
    body: MutationBody | RevisionBody | CommitBody,
    successMessage: string,
    toast?: ToastAction,
  ) {
    if (state.phase === 'submitting') return false
    setState({ ...state, phase: 'submitting', announcement: '' })
    try {
      const response = await requestProfileJSON<MutationResponse>(options.transport, path, body)
      if (response.canonical_query !== body.query) {
        throw new Error('The mutation changed analytical browser state.')
      }
      const selection = response.selection.value as SelectionValue
      const projection: ViewProjection = {
        ...response.projection,
        revision: response.revision,
        pending: response.pending,
        canonical_query: body.query,
        selection,
      }
      const viewWarning = await show(projection, body.selection)
      const auditWarning = projection.warnings?.find(
        (warning) => warning.code === 'audit_completion_failed',
      )?.detail
      // The toast's status region is inserted with its text, which screen readers may skip, so
      // the persistent announcement repeats it.
      const toasted = toast && toastMessage(response.operation, toast)
      if (toasted) showFlash(toasted)
      setState({
        ...newestRevision(response),
        phase: 'idle',
        announcement: [
          toasted || mutationAnnouncement(successMessage, response.selection.kind),
          auditWarning,
          viewWarning,
        ]
          .filter(Boolean)
          .join(' '),
        ...(response.provider_write ? { providerWrite: response.provider_write } : {}),
      })
      if (response.provider_write) options.onProviderWrite?.(response.provider_write)
      return true
    } catch (error) {
      await handleFailure(error)
      return false
    }
  }

  // A view change that finished while the request ran keeps its own query: reread that view at
  // the new revision rather than reinstall the query the mutation was sent with. The reread
  // takes the response's selection, such as a cleared one, unless the selection changed too.
  async function show(
    projection: ViewProjection,
    sent: string | undefined,
  ): Promise<string | undefined> {
    const shown = options.host.current()
    if (!shown || shown.canonical_query === projection.canonical_query) {
      options.host.accept(projection)
      return undefined
    }
    try {
      await options.host.refresh(shown.selection === sent ? projection.selection : undefined, true)
      return undefined
    } catch {
      return 'The current view could not be refreshed.'
    }
  }

  // A reread of a moved-on view can install a revision newer than the response, such as after
  // another session's change. Keep it, so the next edit expects the revision on screen.
  function newestRevision(response: MutationResponse): Pick<EditingState, 'revision' | 'pending'> {
    const revision = parseRevision(response.revision)
    const shown = options.host.current()
    if (shown && parseRevision(shown.revision) > revision) {
      return { revision: parseRevision(shown.revision), pending: shown.pending }
    }
    return { revision, pending: response.pending }
  }

  function projectionRevisionIsCurrent(current: ViewProjection): boolean {
    if (parseRevision(current.revision) === state.revision) return true
    setState({
      ...state,
      phase: 'conflict',
      announcement:
        'The displayed profile revision changed. Review the current data and try again.',
    })
    return false
  }

  async function handleFailure(error: unknown): Promise<void> {
    if (!(error instanceof MoneyflowProblem)) {
      setState({ ...state, phase: 'failed', announcement: REQUEST_FAILED })
      return
    }
    const code = error.problem.code
    if (code === 'revision_conflict') {
      if (!(await refreshAfterRejection())) return
      setState({
        ...state,
        phase: 'conflict',
        announcement:
          'The profile changed in another session. Review the current data and try again.',
      })
      return
    }
    if (code === 'selection_stale') {
      const disposition = error.problem.selection
      const selection = disposition?.value as SelectionValue | undefined
      if (!(await refreshAfterRejection(selection))) return
      setState({
        ...state,
        phase: 'idle',
        announcement:
          disposition?.kind === 'refreshed'
            ? 'Selection refreshed. Invoke the action again.'
            : 'Selection cleared. Invoke the action again.',
      })
      return
    }
    setState({ ...state, phase: 'failed', announcement: safeFailureMessage(error.problem) })
  }

  async function refreshAfterRejection(selection?: SelectionValue): Promise<boolean> {
    try {
      const projection = await options.host.refresh(selection, true)
      if (projection) sync(projection)
      return projection !== undefined
    } catch {
      setState({
        ...state,
        phase: 'failed',
        announcement: 'The current profile could not be loaded.',
      })
      return false
    }
  }

  return {
    get state() {
      subscribe()
      return state
    },
    sync,
    submit,
    undo,
    redo,
    commit,
    catalog,
    previewMerchant,
  }
}

function parseRevision(value: string): bigint {
  if (!/^[0-9]+$/.test(value)) throw new Error('The Moneyflow revision is invalid.')
  return BigInt(value)
}

function mutationAnnouncement(message: string, disposition: string): string {
  if (disposition === 'cleared') return `${message} Selection cleared.`
  if (disposition === 'refreshed') return `${message} Selection refreshed.`
  return message
}

/** The message for a refusal. It names the server's reason only from `reason`, never `detail`. */
function safeFailureMessage({ code, reason }: Problem): string {
  if (reason === 'taxonomy_label_collision') return LABEL_TAKEN
  if (code === 'store_busy') return 'The profile is busy. Try the action again when ready.'
  if (code === 'store_error') return 'The profile could not save that action.'
  if (code === 'token_expired') return 'The secure browser session expired. Reload and try again.'
  if (code === 'invalid_origin') return 'Open the canonical Moneyflow URL to make changes.'
  if (code === 'invalid_operation' || code === 'invalid_target') {
    return 'That action is not available for the current target.'
  }
  return 'The profile request was rejected.'
}
