import { createSubscriber } from 'svelte/reactivity'

import {
  MoneyflowProblem,
  requestProfileJSON,
  type MutationFetch,
  type PendingSummary,
  type ReviewBody,
  type ReviewResponse,
  type ReviewTargetsBody,
} from '../api/client'
import type { components } from '../api/schema'

type ReviewOperation = components['schemas']['ReviewOperation']
type ReviewTarget = components['schemas']['ReviewTarget']

export type ReviewPhase = 'idle' | 'loading' | 'conflict' | 'failed'

export interface ReviewState {
  phase: ReviewPhase
  reviewedRevision: bigint | undefined
  pending: PendingSummary
  activeOperations: ReviewOperation[]
  inactiveOperations: ReviewOperation[]
  announcement: string
}

export interface ReviewController {
  readonly state: ReviewState
  load(): Promise<boolean>
  /**
   * Loads an operation's next `reviewTargetPage` affected rows in one request. The review
   * itself carries each active operation's first rows.
   */
  loadMore(operationID: string): Promise<boolean>
  /** The operation's affected rows loaded so far. */
  targets(operationID: string): ReviewTarget[]
  /** True while the operation's rows load. */
  targetsLoading(operationID: string): boolean
  isStale(): boolean
  clear(): void
}

interface ReviewControllerOptions {
  transport: MutationFetch
  revision(): bigint
}

/** The server's largest target window: the rows one Show more request adds. */
export const reviewTargetPage = 400
const emptyPending: PendingSummary = {
  active_operations: 0,
  inactive_operations: 0,
  affected_transactions: 0,
}

export function createReviewController(options: ReviewControllerOptions): ReviewController {
  let state: ReviewState = {
    phase: 'idle',
    reviewedRevision: undefined,
    pending: emptyPending,
    activeOperations: [],
    inactiveOperations: [],
    announcement: '',
  }
  const loadedTargets = new Map<string, ReviewTarget[]>()
  const loadingTargets = new Set<string>()
  /** Advances when the review reloads or clears, so rows for the old review are dropped. */
  let generation = 0
  let notify = (): void => undefined
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => {
      notify = () => undefined
    }
  })

  function setState(next: ReviewState): void {
    state = next
    notify()
  }

  function resetTargets(): void {
    generation += 1
    loadedTargets.clear()
    loadingTargets.clear()
  }

  async function load(): Promise<boolean> {
    const expected = options.revision()
    resetTargets()
    setState({ ...state, phase: 'loading', announcement: '' })
    const body: ReviewBody = { version: '1', expected_revision: expected.toString(10) }
    try {
      const response = await requestProfileJSON<ReviewResponse>(
        options.transport,
        'api/v1/review',
        body,
      )
      const reviewedRevision = parseRevision(response.revision)
      resetTargets()
      for (const operation of response.active_operations ?? []) {
        if (operation.targets?.length) loadedTargets.set(operation.operation_id, operation.targets)
      }
      setState({
        phase: 'idle',
        reviewedRevision,
        pending: response.pending,
        activeOperations: response.active_operations ?? [],
        inactiveOperations: response.inactive_operations ?? [],
        announcement: '',
      })
      return true
    } catch (error) {
      fail(error)
      return false
    }
  }

  async function loadMore(operationID: string): Promise<boolean> {
    const revision = state.reviewedRevision
    if (revision === undefined || loadingTargets.has(operationID)) return false
    const started = generation
    loadingTargets.add(operationID)
    setState({ ...state, phase: 'idle', announcement: '' })
    try {
      const rows = loadedTargets.get(operationID) ?? []
      const body: ReviewTargetsBody = {
        version: '1',
        expected_revision: revision.toString(10),
        operation_id: operationID,
        window: { offset: rows.length, limit: reviewTargetPage },
      }
      const response = await requestProfileJSON<ReviewResponse>(
        options.transport,
        'api/v1/review/targets',
        body,
      )
      if (generation !== started) return false
      loadedTargets.set(operationID, [...rows, ...(response.targets ?? [])])
      return true
    } catch (error) {
      if (generation === started) fail(error)
      return false
    } finally {
      if (generation === started) {
        loadingTargets.delete(operationID)
        notify()
      }
    }
  }

  function fail(error: unknown): void {
    const conflict = error instanceof MoneyflowProblem && error.problem.code === 'revision_conflict'
    setState({
      ...state,
      phase: conflict ? 'conflict' : 'failed',
      announcement: conflict
        ? 'Pending changes changed in another session. Reopen review.'
        : 'Pending changes could not be loaded.',
    })
  }

  function clear(): void {
    resetTargets()
    setState({
      phase: 'idle',
      reviewedRevision: undefined,
      pending: emptyPending,
      activeOperations: [],
      inactiveOperations: [],
      announcement: '',
    })
  }

  return {
    get state() {
      subscribe()
      return state
    },
    load,
    loadMore,
    targets: (operationID) => {
      subscribe()
      return loadedTargets.get(operationID) ?? []
    },
    targetsLoading: (operationID) => {
      subscribe()
      return loadingTargets.has(operationID)
    },
    isStale: () =>
      state.reviewedRevision !== undefined && state.reviewedRevision !== options.revision(),
    clear,
  }
}

function parseRevision(value: string): bigint {
  if (!/^[0-9]+$/.test(value)) throw new Error('The Moneyflow revision is invalid.')
  return BigInt(value)
}
