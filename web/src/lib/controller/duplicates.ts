import { createSubscriber } from 'svelte/reactivity'

import {
  MoneyflowProblem,
  requestProfileJSON,
  type DuplicateResponse,
  type MoneyflowClient,
  type MutationBody,
  type MutationFetch,
  type MutationResponse,
  type SelectionValue,
  type ViewProjection,
} from '../api/client'
import { failureReason, problemReason, showFailure } from '../shell/notices'

export type DuplicatePhase =
  'idle' | 'loading' | 'ready' | 'confirming' | 'submitting' | 'conflict' | 'failed'

export interface DuplicateState {
  phase: DuplicatePhase
  projection?: DuplicateResponse
  cursor: number
  announcement: string
  confirmationCount: number
}

export interface DuplicateHost {
  current(): ViewProjection | undefined
  /**
   * Rereads the view with the selection it shows once view changes on their way have landed. A
   * failure resolves undefined; it also shows in the workspace when `report()` is true by then
   * and nothing newer owns the workspace problem.
   */
  recheck(report: () => boolean): Promise<ViewProjection | undefined>
}

export interface DuplicateController {
  readonly state: DuplicateState
  open(): Promise<boolean>
  close(): void
  move(delta: -1 | 1): void
  focus(index: number): void
  home(): void
  end(): void
  page(delta: -1 | 1): Promise<boolean>
  focused(): DuplicateRow | undefined
  toggleFocused(): Promise<boolean>
  hideFocused(): Promise<boolean>
  requestDelete(): void
  /** Closes the confirmation, even while its deletion is still being sent. */
  cancelDelete(): void
  confirmDelete(): Promise<boolean>
  /** Rereads the view and the duplicate list after a failure. */
  retry(): Promise<boolean>
  /** Stops for good, as when the workspace is gone: a refusal still on its way shows nothing. */
  stop(): void
}

type DuplicateRow = NonNullable<NonNullable<DuplicateResponse['groups']>[number]['rows']>[number]

interface DuplicateControllerOptions {
  client: Pick<MoneyflowClient, 'projectDuplicates' | 'transition'>
  mutations: MutationFetch
  host: DuplicateHost
}

const windowLimit = 200
const refreshFailed = 'The duplicate list could not be refreshed. Press Retry.'
const changeRefused = 'The duplicate change could not be saved.'

export function createDuplicateController(
  options: DuplicateControllerOptions,
): DuplicateController {
  let state: DuplicateState = {
    phase: 'idle',
    cursor: 0,
    announcement: '',
    confirmationCount: 0,
  }
  // Counts closings. Each async call keeps the count it started with and checks `live` after
  // every await, so a late answer cannot change a review that closed, or closed and reopened.
  let closings = 0
  let stopped = false
  let notify = (): void => undefined
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => {
      notify = () => undefined
    }
  })

  function setState(next: DuplicateState): void {
    state = next
    notify()
  }

  /** Whether review is still the one an async call that began in `session` was made for. */
  function live(session: number): boolean {
    return session === closings
  }

  async function open(): Promise<boolean> {
    const current = options.host.current()
    if (!current) return false
    return await load(closings, current, undefined, 0, 0)
  }

  function close(): void {
    closings += 1
    setState({ phase: 'idle', cursor: 0, announcement: '', confirmationCount: 0 })
  }

  async function load(
    session: number,
    current: ViewProjection,
    selectionValue?: SelectionValue,
    groupOffset = state.projection?.group_window.offset ?? 0,
    rowOffset = state.projection?.row_window.offset ?? 0,
    refreshStaleSelection = true,
  ): Promise<boolean> {
    setState({ ...state, phase: 'loading', announcement: '' })
    try {
      const projection = await options.client.projectDuplicates(
        {
          version: '1',
          expected_revision: current.revision,
          query: current.canonical_query,
          ...(selectionValue === undefined ? {} : { selection: selectionValue }),
          group_window: { offset: groupOffset, limit: windowLimit },
          row_window: { offset: rowOffset, limit: windowLimit },
        },
        undefined,
      )
      if (!live(session)) return false
      const rows = flatten(projection)
      setState({
        phase: 'ready',
        projection,
        cursor: Math.min(state.cursor, Math.max(0, rows.length - 1)),
        announcement: projection.status ?? '',
        confirmationCount: 0,
      })
      return true
    } catch (error) {
      if (!live(session)) return false
      if (
        refreshStaleSelection &&
        error instanceof MoneyflowProblem &&
        error.problem.code === 'selection_stale' &&
        error.problem.selection
      ) {
        const accepted = await load(
          session,
          current,
          error.problem.selection.value as SelectionValue,
          groupOffset,
          rowOffset,
          false,
        )
        if (accepted && live(session)) {
          setState({
            ...state,
            announcement: 'The profile changed. The stale duplicate selection was cleared.',
          })
        }
        return accepted
      }
      // Without a list to review, a conflict can only be retried: Retry rereads the view first.
      setState({
        ...state,
        phase: isConflict(error) && state.projection ? 'conflict' : 'failed',
        announcement:
          'The duplicate list could not be loaded. Review the current data and try again.',
      })
      return false
    }
  }

  function move(delta: -1 | 1): void {
    if (!canInteract()) return
    const rows = state.projection ? flatten(state.projection) : []
    if (rows.length === 0) return
    setState({ ...state, cursor: Math.min(Math.max(state.cursor + delta, 0), rows.length - 1) })
  }

  function focus(index: number): void {
    if (!canInteract()) return
    const rows = state.projection ? flatten(state.projection) : []
    if (Number.isSafeInteger(index) && index >= 0 && index < rows.length) {
      setState({ ...state, cursor: index })
    }
  }

  function home(): void {
    if (!canInteract()) return
    if (state.projection) setState({ ...state, cursor: 0 })
  }

  function end(): void {
    if (!canInteract()) return
    const rows = state.projection ? flatten(state.projection) : []
    if (rows.length > 0) setState({ ...state, cursor: rows.length - 1 })
  }

  async function page(delta: -1 | 1): Promise<boolean> {
    if (!canInteract()) return false
    const session = closings
    const current = options.host.current()
    const projection = state.projection
    if (!current || !projection) return false
    let groupOffset = projection.group_window.offset
    let rowOffset = projection.row_window.offset
    if (delta > 0) {
      if (rowOffset + projection.row_window.count < projection.window_transactions) {
        rowOffset += projection.row_window.count
      } else {
        const nextGroupOffset = groupOffset + projection.group_window.count
        if (nextGroupOffset >= projection.total_groups) return false
        groupOffset = nextGroupOffset
        rowOffset = 0
      }
    } else if (rowOffset > 0) {
      const nextRowOffset = Math.max(0, rowOffset + delta * windowLimit)
      if (nextRowOffset === rowOffset) return false
      rowOffset = nextRowOffset
    } else {
      if (groupOffset === 0) return false
      groupOffset = Math.max(0, groupOffset - projection.group_window.limit)
      const selected = await load(
        session,
        current,
        projection.selection as SelectionValue,
        groupOffset,
        0,
      )
      if (!selected || !live(session) || !state.projection) return false
      const terminalOffset =
        Math.floor(Math.max(0, state.projection.window_transactions - 1) / windowLimit) *
        windowLimit
      if (terminalOffset === 0) {
        home()
        return true
      }
      const terminal = await load(
        session,
        current,
        state.projection.selection as SelectionValue,
        groupOffset,
        terminalOffset,
      )
      if (!terminal || !live(session)) return false
      home()
      return true
    }
    const selectionValue = projection.selection as SelectionValue
    const accepted = await load(session, current, selectionValue, groupOffset, rowOffset)
    if (!accepted || !live(session)) return false
    home()
    return true
  }

  function focused(): DuplicateRow | undefined {
    return state.projection ? flatten(state.projection)[state.cursor] : undefined
  }

  async function toggleFocused(): Promise<boolean> {
    if (!canInteract()) return false
    const current = options.host.current()
    const projection = state.projection
    const row = focused()
    if (!current || !projection || !row) return false
    const session = closings
    setState({ ...state, phase: 'loading', announcement: '' })
    try {
      const toggled = await options.client.transition({
        query: detailQuery(current.canonical_query),
        selection: projection.selection as SelectionValue,
        action: 'selection.toggle',
        target: row.target,
        window: { offset: 0, limit: windowLimit },
      })
      if (!live(session)) return false
      return await load(
        session,
        current,
        toggled.selection,
        projection.group_window.offset,
        projection.row_window.offset,
      )
    } catch {
      if (!live(session)) return false
      setState({ ...state, phase: 'failed', announcement: 'Selection could not be changed.' })
      return false
    }
  }

  async function hideFocused(): Promise<boolean> {
    if (!canInteract()) return false
    return await submit('transaction.toggle-hidden')
  }

  function requestDelete(): void {
    if (!canInteract()) return
    if (!focused() || !state.projection) return
    const count = state.projection.selection_count > 0 ? state.projection.selection_count : 1
    setState({ ...state, phase: 'confirming', confirmationCount: count, announcement: '' })
  }

  function cancelDelete(): void {
    if (state.phase === 'confirming') {
      setState({ ...state, phase: 'ready', confirmationCount: 0, announcement: '' })
    } else if (state.phase === 'submitting') {
      // The deletion is already on its way; its outcome shows in the list.
      setState({ ...state, confirmationCount: 0 })
    }
  }

  async function confirmDelete(): Promise<boolean> {
    if (state.phase !== 'confirming') return false
    return await submit('transaction.delete')
  }

  async function submit(action: 'transaction.toggle-hidden' | 'transaction.delete') {
    if (action === 'transaction.delete') {
      if (state.phase !== 'confirming') return false
    } else if (!canInteract()) {
      return false
    }
    const current = options.host.current()
    const projection = state.projection
    const row = focused()
    if (!current || !projection || !row) return false
    const session = closings
    const count = projection.selection_count > 0 ? projection.selection_count : 1
    const body: MutationBody = {
      version: '1',
      expected_revision: projection.revision,
      query: detailQuery(current.canonical_query),
      selection: projection.selection,
      action,
      target: row.target,
      input: {},
      window: { offset: 0, limit: windowLimit },
    }
    setState({ ...state, phase: 'submitting', announcement: '' })
    try {
      await requestProfileJSON<MutationResponse>(options.mutations, 'api/v1/mutations', body)
    } catch (error) {
      return await refused(error, session)
    }
    const staged =
      action === 'transaction.delete'
        ? `Staged deletion for ${count} ${transactionWord(count)}. Press w to review and commit.`
        : `Staged visibility change for ${count} ${transactionWord(count)}.`
    const reloaded = await reload(session)
    if (!live(session)) return true
    setState({
      ...state,
      phase: reloaded ? 'ready' : 'failed',
      // The change is staged either way; pressing h again would reverse it.
      announcement: reloaded ? staged : `${staged} ${refreshFailed}`,
      confirmationCount: 0,
    })
    return true
  }

  async function refused(error: unknown, session: number): Promise<boolean> {
    const reloaded = isConflict(error) && (await reload(session))
    if (!live(session)) {
      // Review closed before the refusal could show in it, so only the workspace can say so,
      // unless it is gone too.
      if (!stopped) showFailure(`${changeRefused} ${failureReason(error)}`)
      return false
    }
    if (isConflict(error)) {
      setState({
        ...state,
        phase: reloaded ? 'conflict' : 'failed',
        announcement: reloaded
          ? 'The profile changed. Review the refreshed duplicates. Invoke deletion again.'
          : 'The profile changed, and the duplicate list could not be refreshed. Press Retry.',
        confirmationCount: 0,
      })
      return false
    }
    const reason = error instanceof MoneyflowProblem ? ` ${problemReason(error.problem)}` : ''
    setState({
      ...state,
      phase: 'failed',
      announcement: `The pending change could not be saved.${reason}`,
      confirmationCount: 0,
    })
    return false
  }

  /**
   * Rereads the view, then the duplicate list; false when either fails. When review closed after
   * `session` began, only the view is reread, and the workspace reports a failure, because the
   * dialog that would have shown it is gone.
   */
  async function reload(session: number): Promise<boolean> {
    const refreshed = await options.host.recheck(() => !live(session))
    if (!live(session)) return refreshed !== undefined
    return refreshed !== undefined && (await load(session, refreshed, undefined, 0, 0))
  }

  async function retry(): Promise<boolean> {
    if (state.phase !== 'failed') return false
    const session = closings
    setState({ ...state, phase: 'loading', announcement: '' })
    const reloaded = await reload(session)
    if (!live(session)) return false
    if (reloaded) return true
    setState({ ...state, phase: 'failed', announcement: refreshFailed })
    return false
  }

  function canInteract(): boolean {
    return state.phase === 'ready' || state.phase === 'conflict'
  }

  return {
    get state() {
      subscribe()
      return state
    },
    open,
    close,
    move,
    focus,
    home,
    end,
    page,
    focused,
    toggleFocused,
    hideFocused,
    requestDelete,
    cancelDelete,
    confirmDelete,
    retry,
    stop: () => {
      stopped = true
    },
  }
}

function isConflict(error: unknown): boolean {
  return error instanceof MoneyflowProblem && error.problem.code === 'revision_conflict'
}

function flatten(projection: DuplicateResponse): DuplicateRow[] {
  return (projection.groups ?? []).flatMap((group) => group.rows ?? [])
}

function detailQuery(query: string): string {
  const values = new URLSearchParams(query)
  values.set('mode', 'detail')
  values.delete('subgroup')
  values.sort()
  return values.toString()
}

function transactionWord(count: number): string {
  return count === 1 ? 'transaction' : 'transactions'
}
