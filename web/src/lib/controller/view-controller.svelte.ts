import {
  MoneyflowProblem,
  type MoneyflowClient,
  type SelectionValue,
  type TransitionBody,
  type TransactionInformationResponse,
  type ViewBody,
  type ViewProjection,
} from '../api/client'
import { SvelteMap } from 'svelte/reactivity'
import { applicationURL, normalizeBrowserBasePath } from './base-path'
import { createEditingController, type EditingController } from './editing'
import { createDuplicateController, type DuplicateController } from './duplicates'
import { createExportController, type ExportController } from './export'
import { OwnedHistoryLedger, type MoneyflowHistoryState } from './history'
import type { SearchPreview } from './search'
import { createProviderController, type ProviderController } from './provider'
import { createProviderWriteController, type ProviderWriteController } from './provider-write'
import { createReviewController, type ReviewController } from './review'
import { preserveCursor, WindowCache } from './windows'

const windowSize = 200

export interface TransitionAction {
  action: TransitionBody['action']
  target?: TransitionBody['target']
  search?: TransitionBody['search']
  filters?: TransitionBody['filters']
  dimension?: TransitionBody['dimension']
  currency?: TransitionBody['currency']
  sort_field?: TransitionBody['sort_field']
  period?: TransitionBody['period']
  depth?: TransitionBody['depth']
}

interface ReturnInProgress {
  sequence: number
  projection: ViewProjection
  settle(): void
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
  readonly duplicates: DuplicateController
  readonly export: ExportController
  readonly review: ReviewController
  readonly provider: ProviderController
  readonly providerWrite: ProviderWriteController
  hydrate(): Promise<void>
  recheck(selection?: SelectionValue): Promise<ViewProjection | undefined>
  moveCursor(delta: -1 | 1): Promise<void>
  moveCursorTo(index: number): Promise<void>
  moveHome(): Promise<void>
  apply(action: TransitionAction): Promise<boolean>
  /**
   * Makes later transitions wait for `work`, as they wait for an Esc return. `work` must not
   * await `apply()` or another transition: that transition waits for `work`, so neither ends.
   */
  hold<T>(work: Promise<T>): Promise<T>
  beginSearch(): SearchSnapshot
  previewSearch(search: string): Promise<SearchPreview>
  /** Keeps the preview as a history entry; does nothing once history dropped `snapshot`. */
  commitSearch(snapshot: SearchSnapshot): void
  /** Returns to the view before the search; does nothing once history dropped `snapshot`. */
  restoreSearch(snapshot: SearchSnapshot): void
  restore(event: PopStateEvent): Promise<void>
  retry(): Promise<void>
  reset(): Promise<void>
  transactionInformation?(identity: string): Promise<TransactionInformationResponse | undefined>
}

export interface ViewControllerOptions {
  basePath: string
  client: MoneyflowClient
  history?: History
  location?: Pick<Location, 'search'>
  instance?: string
  prefetch?: boolean
  recheckDebounceMillis?: number
  returnTimeoutMillis?: number
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
  const returnTimeoutMillis = options.returnTimeoutMillis ?? 1000
  const existing = ledger.record(browserHistory.state)

  let projection = $state.raw<ViewProjection | undefined>()
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
  let returning: ReturnInProgress | undefined
  let lateReturn: ReturnInProgress | undefined
  let barrier: Promise<unknown> = Promise.resolve()
  const prefetchRequests = new SvelteMap<string, AbortController>()

  const providerWrite = createProviderWriteController({
    transport: { mutations: options.client.mutations, status: options.client.providerWriteStatus },
    host: {
      current: () => projection,
      accept: (next) => acceptProfileProjection(next, true),
      reload: () => recheckInternal(undefined, true),
    },
  })
  const editing = createEditingController({
    transport: options.client.mutations,
    onProviderWrite: (status) => {
      providerWrite.install(status)
      void providerWrite.poll()
    },
    host: {
      current: () => projection,
      accept: acceptProfileProjection,
      refresh: recheckInternal,
    },
  })
  const duplicates = createDuplicateController({
    client: options.client,
    mutations: options.client.mutations,
    host: {
      current: () => projection,
      recheck: recheckInternal,
    },
  })
  const exportController = createExportController({ client: options.client })
  const review = createReviewController({
    transport: options.client.mutations,
    revision: () => editing.state.revision,
  })
  const provider = createProviderController({
    transport: { mutations: options.client.mutations, status: options.client.providerStatus },
    host: {
      current: () => projection,
      accept: (next) => acceptProfileProjection(next, true),
    },
  })

  async function hydrate(): Promise<void> {
    const query = currentQuery(browserLocation)
    const owned = ledger.read(browserHistory.state)
    const loaded = await requestProjection(
      () =>
        options.client.view(
          viewBody(query, owned?.selection, alignedOffset(owned?.cursorIndex ?? 0)),
          activeRequest?.signal,
        ),
      (next) => accept(next, 'replace', owned),
      hydrate,
    )
    const busiest = loaded && projection ? busiestCurrency(projection) : undefined
    if (busiest)
      await hold(transitionNow({ action: 'view.set-currency', currency: busiest }, 'replace'))
  }

  function hold<T>(work: Promise<T>): Promise<T> {
    const released = work.then(
      () => undefined,
      () => undefined,
    )
    barrier = Promise.all([barrier, released])
    return work
  }

  async function recheck(selection?: SelectionValue): Promise<ViewProjection | undefined> {
    return await recheckInternal(selection, false)
  }

  async function recheckInternal(
    selection?: SelectionValue,
    force = false,
  ): Promise<ViewProjection | undefined> {
    if (!projection) return undefined
    if (!force && selection === undefined && recheckRequest) return await recheckRequest
    if (
      !force &&
      selection === undefined &&
      Date.now() - lastRevisionObservation < recheckDebounceMillis
    ) {
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
        return acceptProfileProjection(next) ? next : projection
      })
    if (selection !== undefined) return await request
    recheckRequest = request.finally(() => {
      recheckRequest = undefined
    })
    return await recheckRequest
  }

  async function apply(action: TransitionAction): Promise<boolean> {
    await barrier
    return await transitionNow(action)
  }

  async function transitionNow(
    action: TransitionAction,
    historyMode?: 'replace',
  ): Promise<boolean> {
    if (!projection) return false
    lateReturn = undefined
    const prior = projection
    const body: TransitionBody = {
      query: prior.canonical_query,
      selection: prior.selection,
      action: action.action,
      window: { offset: alignedOffset(cursorIndex), limit: windowSize },
      ...(action.target === undefined ? {} : { target: action.target }),
      ...(action.search === undefined ? {} : { search: action.search }),
      ...(action.filters === undefined ? {} : { filters: action.filters }),
      ...(action.dimension === undefined ? {} : { dimension: action.dimension }),
      ...(action.currency === undefined ? {} : { currency: action.currency }),
      ...(action.sort_field === undefined ? {} : { sort_field: action.sort_field }),
      ...(action.period === undefined ? {} : { period: action.period }),
      ...(action.depth === undefined ? {} : { depth: action.depth }),
    }
    const run = async () => {
      return await requestProjection(
        async () =>
          ensureVisibleWindow(await options.client.transition(body, activeRequest?.signal)),
        async (next) => {
          if (action.action === 'view.back' || action.action === 'view.back-to') {
            await returnTo(next)
            return
          }
          const selectionOnly = action.action.startsWith('selection.')
          accept(next, historyMode ?? (selectionOnly ? 'replace' : 'push'))
        },
        async () => {
          await run()
        },
      )
    }
    return await run()
  }

  function returnTo(next: ViewProjection): Promise<void> {
    const current = ledger.read(browserHistory.state)
    const delta = ledger.deltaTo(next.canonical_query, browserHistory.state)
    if (current?.query === next.canonical_query || current === undefined || delta === undefined) {
      accept(next, 'replace')
      return Promise.resolve()
    }
    const traversal = new Promise<void>((resolve) => {
      const timer = setTimeout(() => {
        if (returning?.projection !== next) return
        lateReturn = returning
        returning = undefined
        accept(next, 'none')
        resolve()
      }, returnTimeoutMillis)
      returning = {
        sequence: current.sequence + delta,
        projection: next,
        settle: () => {
          clearTimeout(timer)
          resolve()
        },
      }
    })
    barrier = Promise.all([barrier, traversal])
    browserHistory.go(delta)
    return traversal
  }

  async function installReturn(
    returned: ReturnInProgress,
    owned: MoneyflowHistoryState,
  ): Promise<void> {
    generation += 1
    activeRequest?.abort()
    loading = false
    // Later transitions wait for this return, so they must run even if installing it fails.
    try {
      accept(returned.projection, 'replace', owned)
    } finally {
      returned.settle()
    }
    if (alignedOffset(owned.cursorIndex) !== returned.projection.window.offset) {
      await moveToIndex(owned.cursorIndex)
    }
  }

  async function restore(event: PopStateEvent): Promise<void> {
    searchSnapshot = undefined
    const owned = ledger.record(event.state)
    // A late traversal's view is current only until anything else replaces it, such as an edit.
    const late = lateReturn?.projection === projection ? lateReturn : undefined
    const returned = returning ?? late
    returning = undefined
    lateReturn = undefined
    if (returned && owned && returned.sequence === owned.sequence) {
      await installReturn(returned, owned)
      return
    }
    returned?.settle()
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

  async function transactionInformation(
    identity: string,
  ): Promise<TransactionInformationResponse | undefined> {
    if (!projection) return undefined
    try {
      if (!options.client.transactionInformation) return undefined
      return await options.client.transactionInformation({
        version: '1',
        expected_revision: projection.revision,
        query: projection.canonical_query,
        target: { kind: 'transaction', identity },
        match_window: { offset: 0, limit: 20 },
        item_window: { offset: 0, limit: 20 },
      })
    } catch (error) {
      announcement =
        error instanceof MoneyflowProblem
          ? error.problem.detail
          : 'Transaction information could not be loaded.'
      return undefined
    }
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

  async function previewSearch(search: string): Promise<SearchPreview> {
    if (!projection) return 'superseded'
    const query = searchSnapshot?.projection.canonical_query ?? projection.canonical_query
    const selection = searchSnapshot?.projection.selection ?? projection.selection
    let accepted = false
    const request = requestProjection(
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
    const requestGeneration = generation
    await request
    if (accepted) return 'applied'
    if (requestGeneration !== generation || !problem) return 'superseded'
    return problem.code === 'network_error' ? 'failed' : 'rejected'
  }

  function commitSearch(snapshot: SearchSnapshot): void {
    if (!projection || snapshot !== searchSnapshot) return
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
    if (snapshot !== searchSnapshot) return
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
    editing.sync(snapshot.projection)
    provider.sync(snapshot.projection)
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
    const cached = cache.get(
      projection.canonical_query,
      projection.selection,
      projection.revision,
      offset,
    )
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
    onSuccess: (next: ViewProjection) => void | Promise<void>,
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
      await onSuccess(next)
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
    historyMode: 'push' | 'replace' | 'none',
    restored?: MoneyflowHistoryState,
  ): void {
    cache.store(next)
    cache.retainAdjacent(next.canonical_query, next.selection, next.revision, next.window.offset)
    if (restored) sequence = restored.sequence
    projection = next
    lastRevisionObservation = Date.now()
    editing.sync(next)
    provider.sync(next)
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

    if (historyMode === 'none') {
      schedulePrefetch(next)
      return
    }
    if (historyMode === 'push') sequence += 1
    const state = ownedState(next)
    const url = applicationURL(basePath, next.canonical_query)
    if (historyMode === 'push') browserHistory.pushState(state, '', url)
    else browserHistory.replaceState(state, '', url)
    ledger.record(state)
    schedulePrefetch(next)
  }

  function acceptProfileProjection(next: ViewProjection, replaceHistory = false): boolean {
    if (projection && BigInt(next.revision) < BigInt(projection.revision)) return false
    generation += 1
    activeRequest?.abort()
    activeRequest = undefined
    loading = false
    cache.store(next)
    cache.retainAdjacent(next.canonical_query, next.selection, next.revision, next.window.offset)
    projection = next
    lastRevisionObservation = Date.now()
    editing.sync(next)
    provider.sync(next)
    const cursor = preserveCursor(next, cursorIdentity, cursorIndex)
    cursorIdentity = cursor.identity
    cursorIndex = cursor.index
    announcement = next.warnings?.[0]?.detail ?? next.status ?? ''
    problem = undefined
    if (replaceHistory) replaceOwnedHistory()
    schedulePrefetch(next)
    return true
  }

  function setProjectionWindow(next: ViewProjection, requestedIndex: number): void {
    cache.store(next)
    cache.retainAdjacent(next.canonical_query, next.selection, next.revision, next.window.offset)
    projection = next
    lastRevisionObservation = Date.now()
    editing.sync(next)
    provider.sync(next)
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
      if (
        !key.startsWith(
          `${current.canonical_query}\u0000${current.selection}\u0000${current.revision}\u0000`,
        )
      ) {
        request.abort()
        prefetchRequests.delete(key)
      }
    }
    const offsets = [current.window.offset - windowSize, current.window.offset + windowSize]
    for (const offset of offsets) {
      const key = `${current.canonical_query}\u0000${current.selection}\u0000${current.revision}\u0000${offset}`
      if (
        offset < 0 ||
        offset >= current.total_rows ||
        cache.get(current.canonical_query, current.selection, current.revision, offset) ||
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
            projection.selection !== current.selection ||
            projection.revision !== current.revision ||
            next.revision !== current.revision
          )
            return
          cache.store(next)
          cache.retainAdjacent(
            current.canonical_query,
            current.selection,
            current.revision,
            current.window.offset,
          )
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
    duplicates,
    export: exportController,
    review,
    provider,
    providerWrite,
    hydrate,
    recheck,
    moveCursor,
    moveCursorTo,
    moveHome,
    apply,
    hold,
    beginSearch,
    previewSearch,
    commitSearch,
    restoreSearch,
    restore,
    retry,
    reset,
    transactionInformation,
  }
}

function viewBody(query: string, selection: SelectionValue | undefined, offset: number): ViewBody {
  return {
    query,
    ...(selection === undefined ? {} : { selection }),
    window: { offset, limit: windowSize },
  }
}

function busiestCurrency(next: ViewProjection): TransitionAction['currency'] {
  if (next.view.currency) return undefined
  let busiest: ViewProjection['currencies'][number] | undefined
  for (const candidate of next.currencies) {
    if (busiest === undefined || candidate.count > busiest.count) busiest = candidate
  }
  return busiest && { currency: busiest.currency, scale: busiest.scale }
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
