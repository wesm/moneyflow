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
import { showNotice } from '../shell/notices'

export type MutationPhase = 'idle' | 'submitting' | 'conflict' | 'failed'

export interface EditingState {
  revision: bigint
  phase: MutationPhase
  pending: PendingSummary
  announcement: string
}

export interface MutationIntent {
  action: string
  input: MutationInput
  target?: { kind: string; identity: string }
}

export interface EditingProjectionHost {
  current(): ViewProjection | undefined
  /**
   * Marks the view an edit is sent from. The returned function shows the edit's view only while
   * no view change has started since, and says whether it did.
   */
  claim(): (projection: ViewProjection) => boolean
  /** Resolves once view changes queued or on their way have settled. */
  settled(): Promise<void>
  /**
   * Rereads the view once view changes on their way have landed, with the selection `choose`
   * picks from the view shown then; undefined keeps that view's selection.
   */
  refresh(
    choose: (shown: ViewProjection) => SelectionValue | undefined,
  ): Promise<ViewProjection | undefined>
}

/** An editing key's turn, held from the key press until the edit it starts settles. */
export interface KeyTurn {
  /**
   * Runs the key's `work` and gives up the turn once what it returns settles. Edits that `work`
   * starts before its first await are the key's own and are not refused for the turn.
   */
  run(work: () => unknown): Promise<void>
}

export interface EditingController {
  readonly state: EditingState
  /**
   * Holds a turn for an editing key. Edits from dialogs are refused while any key holds one, so
   * none can run between a key press and the key's edit. Returns undefined while a dialog's edit
   * is on its way: the key would otherwise run after it, on a view it did not see.
   */
  holdForKey(): KeyTurn | undefined
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
  /**
   * Stops for good, as when the workspace that sent the edits is gone. An edit still on its way
   * finishes on the server, but its answer changes nothing and shows nothing. From then on, a
   * merchant preview that fails is not sent again, and a failure that arrives changes nothing.
   */
  stop(): void
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
/** The warning the editing controller reports with the commit that caused it. */
export const auditWarningCode = 'audit_completion_failed'

/** Why an edit made while another is on its way was not sent. */
export const EDIT_IN_FLIGHT = 'Wait for the previous edit to finish, then try again.'

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

  // Editing keys pressed and not yet settled, whether waiting for their turn or running.
  let heldKeys = 0
  // True while a key's turn calls its work, so the edits it starts are known as the key's.
  let keyCalling = false
  // Whether the edit on its way was started by a key.
  let sentByKey = false
  let stopped = false

  function holdForKey(): KeyTurn | undefined {
    if (state.phase === 'submitting' && !sentByKey) return undefined
    heldKeys += 1
    return {
      async run(work) {
        try {
          keyCalling = true
          let result: unknown
          try {
            result = work()
          } finally {
            keyCalling = false
          }
          await result
        } finally {
          heldKeys -= 1
        }
      },
    }
  }

  // An edit is built from the revision and view on screen. While another edit is on its way,
  // both may change, so a new edit is refused, visibly, instead of waiting: one made in a dialog
  // could otherwise act on rows its confirmation did not name, or run out of order with a key.
  // A dialog's edit is refused while a key waits for its turn, too: the key reads the view when
  // its turn comes, so the dialog's edit would otherwise go first, such as an undo reversing it.
  function refusedWhileBusy(): boolean {
    const keyWaiting = heldKeys > 0 && !keyCalling
    if (state.phase !== 'submitting' && !keyWaiting) return false
    setState({ ...state, announcement: EDIT_IN_FLIGHT })
    return true
  }

  function sync(projection: ViewProjection): void {
    setState({
      ...state,
      revision: parseRevision(projection.revision),
      pending: projection.pending,
    })
  }

  async function submit(intent: MutationIntent): Promise<boolean> {
    if (refusedWhileBusy()) return false
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
    if (refusedWhileBusy()) return false
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
    if (refusedWhileBusy()) return false
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
          stopped ||
          attempt !== 0 ||
          !(error instanceof MoneyflowProblem) ||
          !['selection_stale', 'revision_conflict'].includes(error.problem.code)
        )
          throw error
        await handleFailure(error, body.selection)
        // The controller can stop while the failure's reread is on its way.
        if (stopped || state.phase === 'failed') throw error
      }
    }
  }

  async function mutate(
    path: string,
    body: MutationBody | RevisionBody | CommitBody,
    successMessage: string,
    toast?: ToastAction,
  ) {
    sentByKey = keyCalling
    setState({ ...state, phase: 'submitting', announcement: '' })
    const install = options.host.claim()
    try {
      const response = await requestProfileJSON<MutationResponse>(options.transport, path, body)
      const selection = response.selection.value as SelectionValue
      // An answer can name another query than the one sent, such as the parent view undo
      // returns to when its drilled target is gone. show() decides which view to install.
      const projection: ViewProjection = {
        ...response.projection,
        revision: response.revision,
        pending: response.pending,
        canonical_query: response.canonical_query,
        selection,
      }
      const viewWarning = await show(projection, body.query, body.selection, install)
      if (stopped) return false
      const warnings = projection.warnings ?? []
      const auditWarning = warnings.find((warning) => warning.code === auditWarningCode)?.detail
      const toasted = toast && toastMessage(response.operation, toast)
      // The status bar shows one notice, so it carries the view's warnings beside the edit's.
      // The server repeats the audit warning until the next commit; it belongs to this one.
      const notice = [
        toasted,
        ...warnings.filter((warning) => warning.code !== auditWarningCode).map((w) => w.detail),
        path === 'api/v1/commit' && auditWarning,
        viewWarning,
      ]
        .filter(Boolean)
        .join(' ')
      if (notice) showNotice(notice)
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
      })
      if (response.provider_write) options.onProviderWrite?.(response.provider_write)
      return true
    } catch (error) {
      if (!stopped) await handleFailure(error, body.selection)
      return false
    }
  }

  // A view change that finished while the request ran keeps its own query: reread that view at
  // the new revision rather than reinstall the query the mutation was sent with. The reread
  // takes the response's selection, such as a cleared one, unless the selection changed too.
  // While the view sent is still on screen, the answer is for it, even under another query,
  // unless a view change is on its way: that change owns the view until it settles. If it left
  // the view and selection the edit was sent with, such as a Back that failed, the answer is
  // still for what is shown; otherwise the view is reread by the same rule.
  async function show(
    projection: ViewProjection,
    sentQuery: string,
    sent: string | undefined,
    install: (projection: ViewProjection) => boolean,
  ): Promise<string | undefined> {
    const shown = options.host.current()
    if (
      !shown ||
      shown.canonical_query === projection.canonical_query ||
      shown.canonical_query === sentQuery
    ) {
      if (install(projection)) return undefined
      await options.host.settled()
      const now = options.host.current()
      if (now?.canonical_query === sentQuery && now.selection === sent) {
        if (options.host.claim()(projection)) return undefined
      }
    }
    // Chosen once view changes on their way have landed, so a selection made since is kept.
    return await reread((now) => (now.selection === sent ? projection.selection : undefined))
  }

  async function reread(
    choose: (shown: ViewProjection) => SelectionValue | undefined,
  ): Promise<string | undefined> {
    try {
      await options.host.refresh(choose)
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

  /** Handles a refused request that was sent with the selection `sent`. */
  async function handleFailure(error: unknown, sent: string | undefined): Promise<void> {
    if (!(error instanceof MoneyflowProblem)) {
      setState({ ...state, phase: 'failed', announcement: REQUEST_FAILED })
      return
    }
    const code = error.problem.code
    if (code === 'revision_conflict') {
      if (!(await refreshAfterRejection(() => undefined))) return
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
      // Chosen once view changes on their way have landed, so a selection made since is kept.
      const choose = (now: ViewProjection) => (now.selection === sent ? selection : undefined)
      if (!(await refreshAfterRejection(choose))) return
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

  async function refreshAfterRejection(
    choose: (shown: ViewProjection) => SelectionValue | undefined,
  ): Promise<boolean> {
    try {
      const projection = await options.host.refresh(choose)
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
    holdForKey,
    sync,
    submit,
    undo,
    redo,
    commit,
    catalog,
    previewMerchant,
    stop: () => {
      stopped = true
    },
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
