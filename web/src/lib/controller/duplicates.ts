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
import { problemReason } from '../shell/notices'

export type DuplicatePhase =
  | 'idle'
  | 'loading'
  | 'ready'
  | 'confirming'
  | 'submitting'
  | 'conflict'
  | 'failed'

export interface DuplicateState {
  phase: DuplicatePhase
  projection?: DuplicateResponse
  cursor: number
  announcement: string
  confirmationCount: number
}

export interface DuplicateHost {
  current(): ViewProjection | undefined
  recheck(selection?: SelectionValue, force?: boolean): Promise<ViewProjection | undefined>
  /** Shows in the workspace that a reread failed, with a Retry that rereads `selection`. */
  reportRecheckFailure(error: unknown, selection?: SelectionValue): void
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
}

type DuplicateRow = NonNullable<NonNullable<DuplicateResponse['groups']>[number]['rows']>[number]

interface DuplicateControllerOptions {
  client: Pick<MoneyflowClient, 'projectDuplicates' | 'transition'>
  mutations: MutationFetch
  host: DuplicateHost
}

const windowLimit = 200
const refreshFailed = 'The duplicate list could not be refreshed. Press Retry.'

export function createDuplicateController(
  options: DuplicateControllerOptions,
): DuplicateController {
  let state: DuplicateState = {
    phase: 'idle',
    cursor: 0,
    announcement: '',
    confirmationCount: 0,
  }
  // Counts closings, so a change sent before review closed can tell its dialog is gone.
  let closings = 0
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

  async function open(): Promise<boolean> {
    const current = options.host.current()
    if (!current) return false
    return await load(current, undefined, 0, 0)
  }

  function close(): void {
    closings += 1
    setState({ phase: 'idle', cursor: 0, announcement: '', confirmationCount: 0 })
  }

  async function load(
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
      if (
        refreshStaleSelection &&
        error instanceof MoneyflowProblem &&
        error.problem.code === 'selection_stale' &&
        error.problem.selection
      ) {
        const accepted = await load(
          current,
          error.problem.selection.value as SelectionValue,
          groupOffset,
          rowOffset,
          false,
        )
        if (accepted) {
          setState({
            ...state,
            announcement: 'The profile changed. The stale duplicate selection was cleared.',
          })
        }
        return accepted
      }
      setState({
        ...state,
        phase:
          error instanceof MoneyflowProblem && error.problem.code === 'revision_conflict'
            ? 'conflict'
            : 'failed',
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
      const selected = await load(current, projection.selection as SelectionValue, groupOffset, 0)
      if (!selected || !state.projection) return selected
      const terminalOffset =
        Math.floor(Math.max(0, state.projection.window_transactions - 1) / windowLimit) *
        windowLimit
      if (terminalOffset === 0) {
        home()
        return true
      }
      const terminal = await load(
        current,
        state.projection.selection as SelectionValue,
        groupOffset,
        terminalOffset,
      )
      if (terminal) home()
      return terminal
    }
    const selectionValue = projection.selection as SelectionValue
    const accepted = await load(current, selectionValue, groupOffset, rowOffset)
    if (accepted) home()
    return accepted
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
    setState({ ...state, phase: 'loading', announcement: '' })
    try {
      const toggled = await options.client.transition({
        query: detailQuery(current.canonical_query),
        selection: projection.selection as SelectionValue,
        action: 'selection.toggle',
        target: row.target,
        window: { offset: 0, limit: windowLimit },
      })
      return await load(
        current,
        toggled.selection,
        projection.group_window.offset,
        projection.row_window.offset,
      )
    } catch {
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
    const mainSelection = current.selection
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
      return await refused(error, unchangedSelection(mainSelection))
    }
    const staged =
      action === 'transaction.delete'
        ? `Staged deletion for ${count} ${transactionWord(count)}. Press w to review and commit.`
        : `Staged visibility change for ${count} ${transactionWord(count)}.`
    const reloaded = await reload(unchangedSelection(mainSelection), session)
    if (session !== closings) return true
    setState({
      ...state,
      phase: reloaded ? 'ready' : 'failed',
      // The change is staged either way; pressing h again would reverse it.
      announcement: reloaded ? staged : `${staged} ${refreshFailed}`,
      confirmationCount: 0,
    })
    return true
  }

  /**
   * The main selection to restore after a change, unless the user changed it while the request
   * ran, such as after closing review. The reread then keeps the newer selection.
   */
  function unchangedSelection(mainSelection: SelectionValue): SelectionValue | undefined {
    return options.host.current()?.selection === mainSelection ? mainSelection : undefined
  }

  async function refused(
    error: unknown,
    mainSelection: SelectionValue | undefined,
  ): Promise<boolean> {
    if (error instanceof MoneyflowProblem && error.problem.code === 'revision_conflict') {
      const reloaded = await reload(mainSelection)
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
   * Rereads the view with `selection`, then the duplicate list; false when either fails. When
   * review closed after `session` began, only the view is reread, and the workspace reports a
   * failure, because the dialog that would have shown it is gone.
   */
  async function reload(selection?: SelectionValue, session = closings): Promise<boolean> {
    let refreshed: ViewProjection | undefined
    try {
      refreshed = await options.host.recheck(selection, true)
    } catch (error) {
      if (session !== closings) options.host.reportRecheckFailure(error, selection)
      return false
    }
    if (session !== closings) return refreshed !== undefined
    return refreshed !== undefined && (await load(refreshed, undefined, 0, 0))
  }

  async function retry(): Promise<boolean> {
    if (state.phase !== 'failed') return false
    setState({ ...state, phase: 'loading', announcement: '' })
    if (await reload()) return true
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
  }
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
