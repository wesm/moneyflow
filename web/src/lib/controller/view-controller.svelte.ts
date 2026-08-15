import {
  MoneyflowProblem,
  type MoneyflowClient,
  type SelectionValue,
  type TransitionBody,
  type ViewBody,
  type ViewProjection,
} from '../api/client'
import { SvelteMap } from 'svelte/reactivity'
import { applicationURL, normalizeBrowserBasePath } from './base-path'
import { createEditingController, type EditingController } from './editing'
import { OwnedHistoryLedger, type MoneyflowHistoryState } from './history'
import { createReviewController, type ReviewController } from './review'
import { preserveCursor, WindowCache } from './windows'

const windowSize = 200

export interface TransitionAction {
  action: TransitionBody['action']
  target?: TransitionBody['target']
  search?: TransitionBody['search']
  filters?: TransitionBody['filters']
}

export interface ControllerProblem {
  kind: 'invalid-view' | 'request'
  code: string
}

export interface SearchSnapshot {
  projection: ViewProjection
  history: MoneyflowHistoryState
  cursorIdentity: string | undefined
  cursorIndex: number
  scrollTop: number
}

export interface ViewController {
  readonly projection: ViewProjection | undefined
  readonly loading: boolean
  readonly announcement: string
  readonly cursorIdentity: string | undefined
  readonly cursorIndex: number
  readonly problem: ControllerProblem | undefined
  readonly editing: EditingController
  readonly review: ReviewController
  hydrate(): Promise<void>
  recheck(selection?: SelectionValue): Promise<ViewProjection | undefined>
  moveCursor(delta: -1 | 1): Promise<void>
  moveCursorTo(index: number): Promise<void>
  moveHome(): Promise<void>
  apply(action: TransitionAction): Promise<boolean>
  beginSearch(): SearchSnapshot
  previewSearch(search: string): Promise<boolean>
  commitSearch(snapshot: SearchSnapshot): void
  restoreSearch(snapshot: SearchSnapshot): void
  restore(event: PopStateEvent): Promise<void>
  retry(): Promise<void>
  reset(): Promise<void>
}

export interface ViewControllerOptions {
  basePath: string
  client: MoneyflowClient
  history?: History
  location?: Pick<Location, 'search'>
  instance?: string
  prefetch?: boolean
  recheckDebounceMillis?: number
}

export function createViewController(options: ViewControllerOptions): ViewController {
  const basePath = normalizeBrowserBasePath(options.basePath)
  const browserHistory = options.history ?? history
  const browserLocation = options.location ?? location
  const instance = options.instance ?? crypto.randomUUID()
  const ledger = new OwnedHistoryLedger(instance)
  const cache = new WindowCache(windowSize)
  const prefetchEnabled = options.prefetch ?? true
  const recheckDebounceMillis = options.recheckDebounceMillis ?? 500
  const existing = ledger.record(browserHistory.state)

  let projection = $state<ViewProjection | undefined>()
  let loading = $state(false)
  let announcement = $state('')
  let cursorIdentity = $state<string | undefined>()
  let cursorIndex = $state(0)
  let scrollTop = 0
  let problem = $state<ControllerProblem | undefined>()
  let sequence = existing?.sequence ?? 0
  let generation = 0
  let activeRequest: AbortController | undefined
  let lastRetry: (() => Promise<void>) | undefined
  let searchSnapshot: SearchSnapshot | undefined
  let recheckRequest: Promise<ViewProjection | undefined> | undefined
  let lastRevisionObservation = 0
  const prefetchRequests = new SvelteMap<string, AbortController>()

  const editing = createEditingController({
    transport: options.client.mutations,
    host: {
      current: () => projection,
      accept: acceptProfileProjection,
      refresh: recheck,
    },
  })
  const review = createReviewController({
    transport: options.client.mutations,
    revision: () => editing.state.revision,
  })

  async function hydrate(): Promise<void> {
    const query = currentQuery(browserLocation)
    const owned = ledger.read(browserHistory.state)
    await requestProjection(
      () =>
        options.client.view(
          viewBody(query, owned?.selection, alignedOffset(owned?.cursorIndex ?? 0)),
          activeRequest?.signal,
        ),
      (next) => accept(next, 'replace', owned),
      hydrate,
    )
  }

  async function recheck(selection?: SelectionValue): Promise<ViewProjection | undefined> {
    if (!projection) return undefined
    if (selection === undefined && recheckRequest) return await recheckRequest
    if (selection === undefined && Date.now() - lastRevisionObservation < recheckDebounceMillis) {
      return projection
    }
    const current = projection
    const request = options.client
      .view(
        viewBody(
          current.canonical_query,
          selection ?? current.selection,
          alignedOffset(cursorIndex),
        ),
      )
      .then((next) => {
        if (projection !== current) return projection
        if (next.revision === current.revision && next.selection === current.selection) {
          return projection
        }
        acceptProfileProjection(next)
        editing.sync(next)
        return next
      })
    if (selection !== undefined) return await request
    recheckRequest = request.finally(() => {
      recheckRequest = undefined
    })
    return await recheckRequest
  }

  async function apply(action: TransitionAction): Promise<boolean> {
    if (!projection) return false
    const prior = projection
    const body: TransitionBody = {
      query: prior.canonical_query,
      selection: prior.selection,
      action: action.action,
      window: { offset: alignedOffset(cursorIndex), limit: windowSize },
      ...(action.target === undefined ? {} : { target: action.target }),
      ...(action.search === undefined ? {} : { search: action.search }),
      ...(action.filters === undefined ? {} : { filters: action.filters }),
    }
    const run = async () => {
      return await requestProjection(
        async () =>
          ensureVisibleWindow(await options.client.transition(body, activeRequest?.signal)),
        (next) => {
          if (action.action === 'view.back') {
            const delta = ledger.deltaTo(next.canonical_query, next.selection, browserHistory.state)
            if (delta !== undefined) {
              browserHistory.go(delta)
              return
            }
            accept(next, 'replace')
            return
          }
          accept(next, action.action.startsWith('selection.') ? 'replace' : 'push')
        },
        async () => {
          await run()
        },
      )
    }
    return await run()
  }

  async function restore(event: PopStateEvent): Promise<void> {
    searchSnapshot = undefined
    const owned = ledger.record(event.state)
    const query = owned?.query ?? currentQuery(browserLocation)
    const selection = owned?.selection ?? projection?.selection
    await requestProjection(
      async () =>
        ensureVisibleWindow(
          await options.client.view(
            viewBody(query, selection, alignedOffset(owned?.cursorIndex ?? 0)),
            activeRequest?.signal,
          ),
        ),
      (next) => accept(next, 'replace', owned),
      () => restore(event),
    )
  }

  async function retry(): Promise<void> {
    await lastRetry?.()
  }

  async function reset(): Promise<void> {
    browserHistory.replaceState(null, '', basePath)
    await hydrate()
  }

  function beginSearch(): SearchSnapshot {
    if (!projection) throw new Error('Cannot open search before a view is loaded.')
    searchSnapshot = {
      projection,
      history: ownedState(projection),
      cursorIdentity,
      cursorIndex,
      scrollTop,
    }
    return searchSnapshot
  }

  async function previewSearch(search: string): Promise<boolean> {
    if (!projection) return false
    const query = searchSnapshot?.projection.canonical_query ?? projection.canonical_query
    const selection = searchSnapshot?.projection.selection ?? projection.selection
    let accepted = false
    await requestProjection(
      async () =>
        ensureVisibleWindow(
          await options.client.transition(
            {
              query,
              selection,
              action: 'search.apply',
              search,
              window: { offset: alignedOffset(cursorIndex), limit: windowSize },
            },
            activeRequest?.signal,
          ),
        ),
      (next) => {
        accept(next, 'replace')
        accepted = true
      },
      () => previewSearch(search).then(() => undefined),
    )
    return accepted
  }

  function commitSearch(snapshot: SearchSnapshot): void {
    if (!projection) return
    const accepted = projection
    browserHistory.replaceState(
      snapshot.history,
      '',
      applicationURL(basePath, snapshot.projection.canonical_query),
    )
    ledger.record(snapshot.history)
    sequence = snapshot.history.sequence
    accept(accepted, 'push')
    searchSnapshot = undefined
  }

  function restoreSearch(snapshot: SearchSnapshot): void {
    generation += 1
    activeRequest?.abort()
    loading = false
    lastRetry = undefined
    projection = snapshot.projection
    cursorIdentity = snapshot.cursorIdentity
    cursorIndex = snapshot.cursorIndex
    scrollTop = snapshot.scrollTop
    problem = undefined
    announcement = snapshot.projection.status ?? ''
    sequence = snapshot.history.sequence
    cache.store(snapshot.projection)
    browserHistory.replaceState(
      snapshot.history,
      '',
      applicationURL(basePath, snapshot.projection.canonical_query),
    )
    ledger.record(snapshot.history)
    searchSnapshot = undefined
  }

  async function moveCursor(delta: -1 | 1): Promise<void> {
    if (!projection || projection.total_rows === 0) return
    const nextIndex = Math.min(Math.max(cursorIndex + delta, 0), projection.total_rows - 1)
    if (nextIndex === cursorIndex) return
    await moveToIndex(nextIndex)
  }

  async function moveHome(): Promise<void> {
    if (!projection || projection.total_rows === 0 || cursorIndex === 0) return
    await moveToIndex(0)
  }

  async function moveCursorTo(index: number): Promise<void> {
    if (!projection || projection.total_rows === 0 || !Number.isSafeInteger(index)) return
    await moveToIndex(Math.min(Math.max(index, 0), projection.total_rows - 1))
  }

  async function moveToIndex(nextIndex: number): Promise<void> {
    if (!projection) return
    const offset = alignedOffset(nextIndex)
    const cached = cache.get(projection.canonical_query, projection.selection, offset)
    if (cached) {
      setProjectionWindow(cached, nextIndex)
      replaceOwnedHistory()
      return
    }
    const query = projection.canonical_query
    const selection = projection.selection
    await requestProjection(
      () => options.client.view(viewBody(query, selection, offset), activeRequest?.signal),
      (next) => {
        setProjectionWindow(next, nextIndex)
        replaceOwnedHistory()
        schedulePrefetch(next)
      },
      () => moveToIndex(nextIndex),
    )
  }

  async function requestProjection(
    request: () => Promise<ViewProjection>,
    onSuccess: (next: ViewProjection) => void,
    retry: () => Promise<void>,
  ): Promise<boolean> {
    lastRetry = async () => {
      await retry()
    }
    generation += 1
    const requestGeneration = generation
    activeRequest?.abort()
    activeRequest = new AbortController()
    loading = true
    problem = undefined
    try {
      const next = await request()
      if (requestGeneration !== generation) return false
      onSuccess(next)
      return true
    } catch (error) {
      if (requestGeneration !== generation || isAbort(error)) return false
      setProblem(error)
      return false
    } finally {
      if (requestGeneration === generation) loading = false
    }
  }

  function accept(
    next: ViewProjection,
    historyMode: 'push' | 'replace',
    restored?: MoneyflowHistoryState,
  ): void {
    cache.store(next)
    cache.retainAdjacent(next.canonical_query, next.selection, next.window.offset)
    if (restored) sequence = restored.sequence
    projection = next
    lastRevisionObservation = Date.now()
    editing.sync(next)
    const cursor = preserveCursor(
      next,
      restored?.cursorIdentity ?? cursorIdentity,
      restored?.cursorIndex ?? cursorIndex,
    )
    cursorIdentity = cursor.identity
    cursorIndex = cursor.index
    scrollTop = restored?.scrollTop ?? scrollTop
    announcement = next.warnings?.[0]?.detail ?? next.status ?? ''
    problem = undefined

    if (historyMode === 'push') sequence += 1
    const state = ownedState(next)
    const url = applicationURL(basePath, next.canonical_query)
    if (historyMode === 'push') browserHistory.pushState(state, '', url)
    else browserHistory.replaceState(state, '', url)
    ledger.record(state)
    schedulePrefetch(next)
  }

  function acceptProfileProjection(next: ViewProjection): void {
    cache.store(next)
    cache.retainAdjacent(next.canonical_query, next.selection, next.window.offset)
    projection = next
    lastRevisionObservation = Date.now()
    const cursor = preserveCursor(next, cursorIdentity, cursorIndex)
    cursorIdentity = cursor.identity
    cursorIndex = cursor.index
    announcement = next.warnings?.[0]?.detail ?? next.status ?? ''
    problem = undefined
    schedulePrefetch(next)
  }

  function setProjectionWindow(next: ViewProjection, requestedIndex: number): void {
    cache.store(next)
    cache.retainAdjacent(next.canonical_query, next.selection, next.window.offset)
    projection = next
    lastRevisionObservation = Date.now()
    editing.sync(next)
    const cursor = preserveCursor(next, undefined, requestedIndex)
    cursorIdentity = cursor.identity
    cursorIndex = cursor.index
  }

  function replaceOwnedHistory(): void {
    if (!projection) return
    const state = ownedState(projection)
    browserHistory.replaceState(state, '', applicationURL(basePath, projection.canonical_query))
    ledger.record(state)
  }

  function ownedState(next: ViewProjection): MoneyflowHistoryState {
    return {
      owner: 'moneyflow-web-v1',
      instance,
      sequence,
      query: next.canonical_query,
      ...(cursorIdentity === undefined ? {} : { cursorIdentity }),
      cursorIndex,
      scrollTop,
      selection: next.selection,
    }
  }

  function schedulePrefetch(current: ViewProjection): void {
    if (!prefetchEnabled) return
    for (const [key, request] of prefetchRequests) {
      if (!key.startsWith(`${current.canonical_query}\u0000${current.selection}\u0000`)) {
        request.abort()
        prefetchRequests.delete(key)
      }
    }
    const offsets = [current.window.offset - windowSize, current.window.offset + windowSize]
    for (const offset of offsets) {
      const key = `${current.canonical_query}\u0000${current.selection}\u0000${offset}`
      if (
        offset < 0 ||
        offset >= current.total_rows ||
        cache.get(current.canonical_query, current.selection, offset) ||
        prefetchRequests.has(key)
      ) {
        continue
      }
      const abort = new AbortController()
      prefetchRequests.set(key, abort)
      void options.client
        .view(viewBody(current.canonical_query, current.selection, offset), abort.signal)
        .then((next) => {
          if (
            projection?.canonical_query !== current.canonical_query ||
            projection.selection !== current.selection
          )
            return
          cache.store(next)
          cache.retainAdjacent(current.canonical_query, current.selection, current.window.offset)
        })
        .catch(() => undefined)
        .finally(() => prefetchRequests.delete(key))
    }
  }

  async function ensureVisibleWindow(next: ViewProjection): Promise<ViewProjection> {
    if (next.total_rows === 0 || next.window.count > 0 || next.window.offset === 0) return next
    return await options.client.view(
      viewBody(next.canonical_query, next.selection, alignedOffset(next.total_rows - 1)),
      activeRequest?.signal,
    )
  }

  function setProblem(error: unknown): void {
    if (error instanceof MoneyflowProblem) {
      const invalid = error.problem.code === 'invalid_view_state'
      problem = { kind: invalid ? 'invalid-view' : 'request', code: error.problem.code }
      announcement = invalid
        ? 'This Moneyflow link is invalid.'
        : 'Moneyflow could not apply that change. Your previous view is unchanged.'
      return
    }
    problem = { kind: 'request', code: 'network_error' }
    announcement = 'Moneyflow could not load this view. Check the connection and retry.'
  }

  return {
    get projection() {
      return projection
    },
    get loading() {
      return loading
    },
    get announcement() {
      return announcement
    },
    get cursorIdentity() {
      return cursorIdentity
    },
    get cursorIndex() {
      return cursorIndex
    },
    get problem() {
      return problem
    },
    editing,
    review,
    hydrate,
    recheck,
    moveCursor,
    moveCursorTo,
    moveHome,
    apply,
    beginSearch,
    previewSearch,
    commitSearch,
    restoreSearch,
    restore,
    retry,
    reset,
  }
}

function viewBody(query: string, selection: SelectionValue | undefined, offset: number): ViewBody {
  return {
    query,
    ...(selection === undefined ? {} : { selection }),
    window: { offset, limit: windowSize },
  }
}

function currentQuery(browserLocation: Pick<Location, 'search'>): string {
  return browserLocation.search.replace(/^\?/, '')
}

function alignedOffset(index: number): number {
  return Math.floor(Math.max(0, index) / windowSize) * windowSize
}

function isAbort(error: unknown): boolean {
  return (
    (error instanceof DOMException && error.name === 'AbortError') ||
    (typeof error === 'object' && error !== null && 'name' in error && error.name === 'AbortError')
  )
}
