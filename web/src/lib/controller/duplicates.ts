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
  cancelDelete(): void
  confirmDelete(): Promise<boolean>
}

type DuplicateRow = NonNullable<NonNullable<DuplicateResponse['groups']>[number]['rows']>[number]

interface DuplicateControllerOptions {
  client: Pick<MoneyflowClient, 'projectDuplicates' | 'transition'>
  mutations: MutationFetch
  host: DuplicateHost
}

const windowLimit = 200

export function createDuplicateController(
  options: DuplicateControllerOptions,
): DuplicateController {
  let state: DuplicateState = {
    phase: 'idle',
    cursor: 0,
    announcement: '',
    confirmationCount: 0,
  }
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
    setState({ phase: 'idle', cursor: 0, announcement: '', confirmationCount: 0 })
  }

  async function load(
    current: ViewProjection,
    selectionValue?: SelectionValue,
    groupOffset = state.projection?.group_window.offset ?? 0,
    rowOffset = state.projection?.row_window.offset ?? 0,
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
    const rows = state.projection ? flatten(state.projection) : []
    if (rows.length === 0) return
    setState({ ...state, cursor: Math.min(Math.max(state.cursor + delta, 0), rows.length - 1) })
  }

  function focus(index: number): void {
    const rows = state.projection ? flatten(state.projection) : []
    if (Number.isSafeInteger(index) && index >= 0 && index < rows.length) {
      setState({ ...state, cursor: index })
    }
  }

  function home(): void {
    if (state.projection) setState({ ...state, cursor: 0 })
  }

  function end(): void {
    const rows = state.projection ? flatten(state.projection) : []
    if (rows.length > 0) setState({ ...state, cursor: rows.length - 1 })
  }

  async function page(delta: -1 | 1): Promise<boolean> {
    const current = options.host.current()
    const projection = state.projection
    if (!current || !projection) return false
    let groupOffset = projection.group_window.offset
    let rowOffset = projection.row_window.offset
    if (delta > 0 && projection.row_window.count < projection.row_window.limit) {
      const nextGroupOffset = groupOffset + projection.group_window.count
      if (nextGroupOffset >= projection.total_groups) return false
      groupOffset = nextGroupOffset
      rowOffset = 0
    } else if (delta < 0 && rowOffset === 0) {
      if (groupOffset === 0) return false
      groupOffset = Math.max(0, groupOffset - projection.group_window.limit)
    } else {
      const nextRowOffset = Math.max(0, rowOffset + delta * windowLimit)
      if (nextRowOffset === rowOffset) return false
      rowOffset = nextRowOffset
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
    const current = options.host.current()
    const projection = state.projection
    const row = focused()
    if (!current || !projection || !row) return false
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
    return await submit('transaction.toggle-hidden')
  }

  function requestDelete(): void {
    if (!focused() || !state.projection) return
    const count = state.projection.selection_count > 0 ? state.projection.selection_count : 1
    setState({ ...state, phase: 'confirming', confirmationCount: count, announcement: '' })
  }

  function cancelDelete(): void {
    if (state.phase === 'confirming') {
      setState({ ...state, phase: 'ready', confirmationCount: 0, announcement: '' })
    }
  }

  async function confirmDelete(): Promise<boolean> {
    if (state.phase !== 'confirming') return false
    return await submit('transaction.delete')
  }

  async function submit(action: 'transaction.toggle-hidden' | 'transaction.delete') {
    const current = options.host.current()
    const projection = state.projection
    const row = focused()
    if (!current || !projection || !row) return false
    const mainSelection = current.selection
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
      const refreshed = await options.host.recheck(mainSelection, true)
      if (!refreshed || !(await load(refreshed, undefined, 0, 0))) {
        setState({
          ...state,
          phase: 'failed',
          announcement: 'The current profile could not be loaded.',
        })
        return false
      }
      setState({
        ...state,
        phase: 'ready',
        announcement:
          action === 'transaction.delete'
            ? `Staged deletion for ${count} ${transactionWord(count)}. Press w to review and commit.`
            : `Staged visibility change for ${count} ${transactionWord(count)}.`,
        confirmationCount: 0,
      })
      return true
    } catch (error) {
      if (error instanceof MoneyflowProblem && error.problem.code === 'revision_conflict') {
        const refreshed = await options.host.recheck(mainSelection, true)
        if (refreshed) await load(refreshed, undefined, 0, 0)
        setState({
          ...state,
          phase: 'conflict',
          announcement:
            'The profile changed. Review the refreshed duplicates. Invoke deletion again.',
          confirmationCount: 0,
        })
        return false
      }
      setState({
        ...state,
        phase: 'failed',
        announcement: 'The pending change could not be saved.',
        confirmationCount: 0,
      })
      return false
    }
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
