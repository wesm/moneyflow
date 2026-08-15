import { createSubscriber } from 'svelte/reactivity'

import {
  MoneyflowProblem,
  requestProfileJSON,
  type CommitBody,
  type MutationBody,
  type MutationFetch,
  type MutationInput,
  type MutationResponse,
  type PendingSummary,
  type RevisionBody,
  type SelectionValue,
  type ViewProjection,
} from '../api/client'

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
  accept(projection: ViewProjection): void
  refresh(selection?: SelectionValue): Promise<ViewProjection | undefined>
}

export interface EditingController {
  readonly state: EditingState
  sync(projection: ViewProjection): void
  submit(intent: MutationIntent): Promise<boolean>
  undo(): Promise<boolean>
  redo(): Promise<boolean>
  commit(reviewedRevision: bigint): Promise<boolean>
}

interface EditingControllerOptions {
  transport: MutationFetch
  host: EditingProjectionHost
}

const emptyPending: PendingSummary = {
  active_operations: 0,
  inactive_operations: 0,
  affected_transactions: 0,
}

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
    return await mutate('api/v1/mutations', body)
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
    const body: RevisionBody = {
      version: '1',
      expected_revision: state.revision.toString(10),
      query: current.canonical_query,
      selection: current.selection,
      window: { offset: current.window.offset, limit: current.window.limit },
    }
    return await mutate(path, body)
  }

  async function commit(reviewedRevision: bigint): Promise<boolean> {
    const current = options.host.current()
    if (!current) return false
    const body: CommitBody = {
      version: '1',
      expected_revision: state.revision.toString(10),
      reviewed_revision: reviewedRevision.toString(10),
      query: current.canonical_query,
      selection: current.selection,
      window: { offset: current.window.offset, limit: current.window.limit },
    }
    return await mutate('api/v1/commit', body)
  }

  async function mutate(path: string, body: MutationBody | RevisionBody | CommitBody) {
    if (state.phase === 'submitting') return false
    setState({ ...state, phase: 'submitting', announcement: '' })
    try {
      const response = await requestProfileJSON<MutationResponse>(options.transport, path, body)
      if (response.canonical_query !== body.query) {
        throw new Error('The mutation changed analytical browser state.')
      }
      const revision = parseRevision(response.revision)
      const selection = response.selection.value as SelectionValue
      const projection: ViewProjection = {
        ...response.projection,
        revision: response.revision,
        pending: response.pending,
        canonical_query: body.query,
        selection,
      }
      options.host.accept(projection)
      setState({
        revision,
        phase: 'idle',
        pending: response.pending,
        announcement: mutationAnnouncement(response.selection.kind),
      })
      return true
    } catch (error) {
      await handleFailure(error)
      return false
    }
  }

  async function handleFailure(error: unknown): Promise<void> {
    if (!(error instanceof MoneyflowProblem)) {
      setState({ ...state, phase: 'failed', announcement: 'The profile request failed.' })
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
    setState({ ...state, phase: 'failed', announcement: safeFailureMessage(code) })
  }

  async function refreshAfterRejection(selection?: SelectionValue): Promise<boolean> {
    try {
      const projection = await options.host.refresh(selection)
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
  }
}

function parseRevision(value: string): bigint {
  if (!/^[0-9]+$/.test(value)) throw new Error('The Moneyflow revision is invalid.')
  return BigInt(value)
}

function mutationAnnouncement(disposition: string): string {
  if (disposition === 'cleared') return 'Change saved as pending. Selection cleared.'
  if (disposition === 'refreshed') return 'Change saved as pending. Selection refreshed.'
  return 'Change saved as pending.'
}

function safeFailureMessage(code: string): string {
  if (code === 'store_busy') return 'The profile is busy. Try the action again when ready.'
  if (code === 'store_error') return 'The profile could not save that action.'
  if (code === 'token_expired') return 'The secure browser session expired. Reload and try again.'
  if (code === 'invalid_operation' || code === 'invalid_target') {
    return 'That action is not available for the current target.'
  }
  return 'The profile request was rejected.'
}
