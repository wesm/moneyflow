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
  loadTargets(operationID: string, offset: number): Promise<boolean>
  targets(operationID: string, offset: number): ReviewTarget[]
  targetOffsets(operationID: string): number[]
  isStale(): boolean
  clear(): void
}

interface ReviewControllerOptions {
  transport: MutationFetch
  revision(): bigint
}

const targetWindowSize = 100
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
  const targetWindows = new Map<string, Map<number, ReviewTarget[]>>()
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

  async function load(): Promise<boolean> {
    const expected = options.revision()
    setState({ ...state, phase: 'loading', announcement: '' })
    const body: ReviewBody = { version: '1', expected_revision: expected.toString(10) }
    try {
      const response = await requestProfileJSON<ReviewResponse>(
        options.transport,
        'api/v1/review',
        body,
      )
      const reviewedRevision = parseRevision(response.revision)
      targetWindows.clear()
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

  async function loadTargets(operationID: string, offset: number): Promise<boolean> {
    if (state.reviewedRevision === undefined || !Number.isSafeInteger(offset) || offset < 0) {
      return false
    }
    const aligned = Math.floor(offset / targetWindowSize) * targetWindowSize
    const body: ReviewTargetsBody = {
      version: '1',
      expected_revision: state.reviewedRevision.toString(10),
      operation_id: operationID,
      window: { offset: aligned, limit: targetWindowSize },
    }
    setState({ ...state, phase: 'loading', announcement: '' })
    try {
      const response = await requestProfileJSON<ReviewResponse>(
        options.transport,
        'api/v1/review/targets',
        body,
      )
      let windows = targetWindows.get(operationID)
      if (!windows) {
        windows = new Map()
        targetWindows.set(operationID, windows)
      }
      windows.set(response.window.offset, response.targets ?? [])
      for (const candidate of windows.keys()) {
        if (Math.abs(candidate - response.window.offset) > targetWindowSize)
          windows.delete(candidate)
      }
      for (const candidate of targetWindows.keys()) {
        if (candidate !== operationID) targetWindows.delete(candidate)
      }
      setState({ ...state, phase: 'idle' })
      return true
    } catch (error) {
      fail(error)
      return false
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
    targetWindows.clear()
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
    loadTargets,
    targets: (operationID, offset) => targetWindows.get(operationID)?.get(offset) ?? [],
    targetOffsets: (operationID) =>
      [...(targetWindows.get(operationID)?.keys() ?? [])].sort((a, b) => a - b),
    isStale: () =>
      state.reviewedRevision !== undefined && state.reviewedRevision !== options.revision(),
    clear,
  }
}

function parseRevision(value: string): bigint {
  if (!/^[0-9]+$/.test(value)) throw new Error('The Moneyflow revision is invalid.')
  return BigInt(value)
}
