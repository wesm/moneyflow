import {
  MoneyflowProblem,
  type InsightsBody,
  type InsightsResponse,
  type MoneyflowClient,
  type SelectionValue,
  type TransitionBody,
  type TransactionInformationResponse,
  type ViewBody,
  type ViewProjection,
} from '../api/client'
import { SvelteMap } from 'svelte/reactivity'
import { auditWarningCode, createEditingController, type EditingController } from './editing'
import { createDuplicateController, type DuplicateController } from './duplicates'
import { createExportController, type ExportController } from './export'
import { OwnedHistoryLedger, type MoneyflowHistoryState } from './history'
import type { SearchPreview } from './search'
import { createProviderController, type ProviderController } from './provider'
import { createProviderWriteController, type ProviderWriteController } from './provider-write'
import { readOnlyClient } from './read-only'
import { createReviewController, type ReviewController } from './review'
import type { ViewStateAdapter } from './view-state'
import {
  INVALID_RESPONSE,
  NETWORK_FAILURE,
  problemReason,
  showFailure,
  showNotice,
} from '../shell/notices'
import { preserveCursor, startsAtFirstRow, WindowCache } from './windows'

const windowSize = 200
// Browsers limit history writes (Safari: 100 in 10 seconds), so cursor moves wait for a pause.
const defaultHistoryDebounceMillis = 300
const changeFailed = 'Moneyflow could not apply that change; the view is unchanged.'
const recheckFailed = 'Moneyflow could not check for newer data; the view may be out of date.'

export interface TransitionAction {
  action: TransitionBody['action']
  target?: TransitionBody['target']
  search?: TransitionBody['search']
  /** The toggles to apply; the transition keeps the period of the view it starts from. */
  filters?: Omit<NonNullable<TransitionBody['filters']>, 'date_range'>
  dimension?: TransitionBody['dimension']
  currency?: TransitionBody['currency']
  sort_field?: TransitionBody['sort_field']
  period?: TransitionBody['period']
  depth?: TransitionBody['depth']
}

/** A window load for a row scrolling shows; `index` is the latest row asked for in it. */
interface ShowRequest {
  offset: number
  index: number
  abort: AbortController
  superseded(): boolean
}

interface ReturnInProgress {
  sequence: number
  projection: ViewProjection
  settle(): void
}

export interface ControllerProblem {
  kind: 'invalid-view' | 'request'
  /** The server's problem code, `network_error`, or `invalid_response` for an unreadable answer. */
  code: string
  /** Why the request failed, in words the user can read. */
  detail: string
}

export interface SearchSnapshot {
  projection: ViewProjection
  history: MoneyflowHistoryState
  cursorIdentity: string | undefined
  cursorIndex: number
}

export interface ViewController {
  readonly projection: ViewProjection | undefined
  readonly loading: boolean
  /** The first load is still setting the view's currency; a search waits for it. */
  readonly opening: boolean
  readonly announcement: string
  readonly cursorIdentity: string | undefined
  readonly cursorIndex: number
  /** Cached windows of the current view next to the loaded one, so the table can draw them. */
  readonly neighbors: readonly ViewProjection[]
  readonly problem: ControllerProblem | undefined
  readonly editing: EditingController
  readonly duplicates: DuplicateController
  readonly export: ExportController
  readonly review: ReviewController
  readonly provider: ProviderController
  readonly providerWrite: ProviderWriteController
  /** Where the view's query and history state live; it reports back and forward. */
  readonly viewState: ViewStateAdapter
  hydrate(): Promise<void>
  /**
   * Rereads the view when the profile may have changed elsewhere. A failure shows a banner and
   * the problem, and resolves `undefined`.
   */
  recheck(selection?: SelectionValue): Promise<ViewProjection | undefined>
  moveCursor(delta: -1 | 1): Promise<void>
  moveCursorTo(index: number): Promise<void>
  moveHome(): Promise<void>
  /**
   * Loads the aligned window holding row `index` when scrolling shows it, without a history
   * entry. If that window does not hold the cursor, the cursor moves to `index`. A row of the
   * loaded window withdraws a window still loading.
   */
  showIndex(index: number): Promise<void>
  /**
   * Loads the window holding the aggregate row `identity` and moves the cursor to it, without a
   * history entry, after earlier transitions finish. Does nothing when the row is not in the view.
   */
  showAnchor(identity: string): Promise<void>
  /**
   * Refetches the view at once when insights report `revision`, newer than the projection's, so
   * the charts can show them. Does nothing for an equal or older revision.
   */
  recheckNewer(revision: string): Promise<void>
  /**
   * Loads the view again after the server's day changed, after earlier transitions finish. Time
   * rows end at today, so the rows can change while the revision stays the same.
   */
  reloadDay(): Promise<void>
  /** Sends one insights request for this profile; the charts' cache decides what to ask. */
  insights(body: InsightsBody, signal: AbortSignal): Promise<InsightsResponse>
  apply(action: TransitionAction): Promise<boolean>
  /**
   * Makes later transitions wait for `work`, as they wait for an Esc return. `work` must not
   * await `apply()` or another transition: that transition waits for `work`, so neither ends.
   */
  hold<T>(work: Promise<T>): Promise<T>
  /** Settles once the transitions queued so far, and any Esc return or held work, finish. */
  settled(): Promise<void>
  beginSearch(): SearchSnapshot
  previewSearch(search: string): Promise<SearchPreview>
  /** Keeps the preview as a history entry; does nothing once history dropped `snapshot`. */
  commitSearch(snapshot: SearchSnapshot): void
  /** Returns to the view before the search; does nothing once history dropped `snapshot`. */
  restoreSearch(snapshot: SearchSnapshot): void
  restore(event: Pick<PopStateEvent, 'state'>): Promise<void>
  /** Writes a cursor position still waiting for a pause to history now, as the page is left. */
  flushHistory(): void
  /**
   * Stops for good, as when the workspace that made this controller is gone. The view request on
   * its way is canceled and its answer dropped. A transition, reload or reread waiting its turn,
   * or asked for later, sends nothing, and a transition resolves false. An edit, duplicate
   * change, transaction information, refresh, provider write or status answer still on its way
   * changes nothing and shows nothing, and an export on its way is canceled. Refresh status
   * polling ends, and history is left alone.
   */
  stop(): void
  retry(): Promise<void>
  reset(): Promise<void>
  transactionInformation?(identity: string): Promise<TransactionInformationResponse | undefined>
}

export interface ViewControllerOptions {
  client: MoneyflowClient
  /** Where the view's query and history state live. */
  viewState: ViewStateAdapter
  /**
   * Offers no change: edits, review, refresh and export are unavailable, and writes never resume.
   */
  readOnly?: boolean
  /** Lets provider status polls start a refresh that is due. */
  autoRefresh?: boolean
  instance?: string
  prefetch?: boolean
  recheckDebounceMillis?: number
  /** How long cursor moves pause before history records them; 0 records each move at once. */
  historyDebounceMillis?: number
  returnTimeoutMillis?: number
}

/** The foreground state a recheck started under; see `failRecheck`. */
interface ForegroundMark {
  generation: number
  failures: number
}

export function createViewController(options: ViewControllerOptions): ViewController {
  let stopped = false
  // Once stopped, history belongs to whatever the host shows instead.
  const viewState: ViewStateAdapter = {
    get state() {
      return options.viewState.state
    },
    get query() {
      return options.viewState.query
    },
    push: (state, query) => {
      if (!stopped) options.viewState.push(state, query)
    },
    replace: (state, query) => {
      if (!stopped) options.viewState.replace(state, query)
    },
    go: (delta) => {
      if (!stopped) options.viewState.go(delta)
    },
    listen: (restore) => options.viewState.listen(restore),
  }
  const client = options.readOnly ? readOnlyClient(options.client) : options.client
  const instance = options.instance ?? crypto.randomUUID()
  const ledger = new OwnedHistoryLedger(instance)
  const cache = new WindowCache(windowSize)
  const prefetchEnabled = options.prefetch ?? true
  const recheckDebounceMillis = options.recheckDebounceMillis ?? 500
  const historyDebounceMillis = options.historyDebounceMillis ?? defaultHistoryDebounceMillis
  const returnTimeoutMillis = options.returnTimeoutMillis ?? 1000
  const existing = ledger.record(viewState.state)

  let projection = $state.raw<ViewProjection | undefined>()
  let loading = $state(false)
  let opening = $state(false)
  let announcement = $state('')
  let cursorIdentity = $state<string | undefined>()
  let cursorIndex = $state(0)
  let problem = $state<ControllerProblem | undefined>()
  // The problem shown is a failed recheck, which the next successful recheck clears.
  let recheckProblem = false
  // Counts foreground failures, so a recheck can tell that a newer request failed meanwhile.
  let problemsRecorded = 0
  // Rechecks are numbered as they start; only an answer newer than the last applied one counts.
  let rechecksStarted = 0
  let appliedRecheck = 0
  let sequence = existing?.sequence ?? 0
  let generation = 0
  let activeRequest: AbortController | undefined
  // The foreground request that set `loading` last; a reread waits for it to settle.
  let foregroundRequest: Promise<unknown> = Promise.resolve()
  let lastRetry: (() => Promise<void>) | undefined
  let searchSnapshot: SearchSnapshot | undefined
  let recheckRequest: Promise<ViewProjection | undefined> | undefined
  let lastRevisionObservation = 0
  let returning: ReturnInProgress | undefined
  let lateReturn: ReturnInProgress | undefined
  let barrier: Promise<unknown> = Promise.resolve()
  let showing: ShowRequest | undefined
  let pendingHistory: { state: MoneyflowHistoryState; query: string } | undefined
  let pendingHistoryTimer: ReturnType<typeof setTimeout> | undefined
  // Pending cursors of entries Back or Forward left before the pause, by entry sequence.
  const leftCursors = new SvelteMap<number, MoneyflowHistoryState>()
  // Prefetch fills the cache without changing the projection; this tells the table to redraw.
  let cacheChanges = $state(0)
  let applyTail: Promise<void> = Promise.resolve()
  const prefetchRequests = new SvelteMap<string, AbortController>()

  const providerWrite = createProviderWriteController({
    transport: { mutations: client.mutations, status: client.providerWriteStatus },
    readOnly: options.readOnly === true,
    host: {
      current: () => projection,
      claim: () => claimView(() => true),
      // A reread waits for view changes on their way, so its answer cannot cut them off.
      reload: () => serially(() => ownedRecheck(undefined, true, () => false)),
    },
  })
  const editing = createEditingController({
    transport: client.mutations,
    onProviderWrite: (status) => {
      providerWrite.install(status)
      void providerWrite.poll()
    },
    host: {
      current: () => projection,
      // Undo can answer for the parent of a drilled view that is gone; the address follows it.
      claim: () => claimView((next) => next.canonical_query !== projection?.canonical_query),
      settled: () =>
        serially(async () => {
          while (loading) await foregroundSettling()
        }),
      // A reread waits for view changes on their way, so its answer cannot cut them off.
      refresh: (choose) =>
        serially(async () => {
          while (loading) await foregroundSettling()
          if (!projection) return undefined
          return await ownedRecheck(choose(projection), true, () => false)
        }),
    },
  })
  const duplicates = createDuplicateController({
    client,
    mutations: client.mutations,
    host: {
      current: () => projection,
      recheck: async (report) => await reportedRecheck(undefined, true, report),
    },
  })
  const exportController = createExportController({ client })
  const review = createReviewController({
    transport: client.mutations,
    revision: () => editing.state.revision,
  })
  const provider = createProviderController({
    transport: { mutations: client.mutations, status: client.providerStatus },
    autoRefresh: options.autoRefresh === true,
    host: {
      current: () => projection,
      accept: (next) => acceptProfileProjection(next, true),
      reload: () => ownedRecheck(undefined, true, () => false),
      serially: (work) => serially(work),
    },
  })

  async function hydrate(): Promise<void> {
    const query = viewState.query
    const owned = ledger.read(viewState.state)
    const loaded = await requestProjection(
      () =>
        client.view(
          viewBody(query, owned?.selection, alignedOffset(owned?.cursorIndex ?? 0)),
          activeRequest?.signal,
        ),
      (next) => accept(next, 'replace', owned),
      hydrate,
    )
    if (loaded) await openInBusiestCurrency()
  }

  /** Scopes a view without a currency, such as an empty profile's after its first import. */
  async function openInBusiestCurrency(): Promise<void> {
    const busiest = projection && !opening ? busiestCurrency(projection) : undefined
    if (!busiest) return
    opening = true
    try {
      await hold(transitionNow({ action: 'view.set-currency', currency: busiest }, 'replace'))
    } finally {
      opening = false
    }
  }

  function hold<T>(work: Promise<T>): Promise<T> {
    const released = work.then(
      () => undefined,
      () => undefined,
    )
    barrier = Promise.all([barrier, released])
    return work
  }

  async function settled(): Promise<void> {
    await Promise.all([applyTail, barrier])
  }

  async function recheck(selection?: SelectionValue): Promise<ViewProjection | undefined> {
    return await reportedRecheck(selection, false)
  }

  /**
   * Marks the view a request starts from. The returned function installs the request's answer
   * only while no view change is on its way or has started since, so the answer cannot cut that
   * change off, and says whether it did. A stopped controller installs nothing.
   */
  function claimView(
    replaceHistory: (next: ViewProjection) => boolean,
  ): (next: ViewProjection) => boolean {
    const started = generation
    return (next) =>
      !stopped &&
      !loading &&
      generation === started &&
      acceptProfileProjection(next, replaceHistory(next))
  }

  /** Settles with the foreground request on its way; never rejects. */
  function foregroundSettling(): Promise<void> {
    return foregroundRequest.then(
      () => undefined,
      () => undefined,
    )
  }

  /** Rereads the view; a failure resolves undefined after `ownedRecheck` handles it. */
  async function reportedRecheck(
    selection: SelectionValue | undefined,
    force: boolean,
    report: () => boolean = () => true,
  ): Promise<ViewProjection | undefined> {
    try {
      return await ownedRecheck(selection, force, report)
    } catch {
      return undefined
    }
  }

  /**
   * Rereads the view. Every reread takes this path. A foreground request on its way, such as a
   * browser Back, settles first, and the reread reads the view it left. Each request gets a
   * sequence number when it starts. Only an answer newer than the last one applied may change the
   * recheck outcome: a success clears an older recheck failure, and a failure shows in the
   * workspace when `report()` allows it and no foreground request owns the problem. An answer
   * installs its view only while the view it was read for is still shown and no foreground request
   * has started since. Throws the request's error after handling it.
   */
  async function ownedRecheck(
    selection: SelectionValue | undefined,
    force: boolean,
    report: () => boolean,
  ): Promise<ViewProjection | undefined> {
    // Awaiting only while a request is on its way keeps a reread started just before a view change
    // from reading after it.
    while (loading) await foregroundSettling()
    // Nothing shows a stopped controller's view any more, so it is not reread.
    if (stopped || !projection) return undefined
    if (!force && selection === undefined && recheckRequest) return await recheckRequest
    // After a failed recheck, only the server can say the view is current again.
    if (
      !force &&
      !recheckProblem &&
      selection === undefined &&
      Date.now() - lastRevisionObservation < recheckDebounceMillis
    ) {
      return projection
    }
    const current = projection
    rechecksStarted += 1
    const order = rechecksStarted
    const foreground = { generation, failures: problemsRecorded }
    const request = client
      .view(
        viewBody(
          current.canonical_query,
          selection ?? current.selection,
          alignedOffset(cursorIndex),
        ),
      )
      .then(
        (next) => settleRecheck(order, foreground, current, next),
        (error: unknown) => {
          failRecheck(order, foreground, error, () => reportedRecheck(selection, true), report)
          throw error
        },
      )
    if (selection !== undefined) return await request
    recheckRequest = request.finally(() => {
      recheckRequest = undefined
    })
    return await recheckRequest
  }

  function settleRecheck(
    order: number,
    foreground: ForegroundMark,
    current: ViewProjection,
    next: ViewProjection,
  ): ViewProjection | undefined {
    if (order > appliedRecheck) {
      appliedRecheck = order
      if (recheckProblem) {
        problem = undefined
        recheckProblem = false
      }
    }
    // A newer view, or a foreground request started since, owns what the workspace shows;
    // installing this answer would abort that request.
    if (projection !== current || generation !== foreground.generation) return projection
    if (next.revision === current.revision && next.selection === current.selection) {
      return projection
    }
    // A foreground failure keeps its problem and Retry, even over fresh data.
    return acceptProfileProjection(next, false, problem !== undefined) ? next : projection
  }

  function failRecheck(
    order: number,
    foreground: ForegroundMark,
    error: unknown,
    retry: () => Promise<unknown>,
    report: () => boolean,
  ): void {
    if (order <= appliedRecheck) return
    appliedRecheck = order
    if (isAbort(error) || !report()) return
    // A foreground failure shown, a foreground request on its way, or one that started or
    // failed since, owns the problem and the Retry.
    if (
      (problem !== undefined && !recheckProblem) ||
      loading ||
      generation !== foreground.generation ||
      problemsRecorded !== foreground.failures
    ) {
      return
    }
    lastRetry = async () => {
      await retry()
    }
    recordProblem(error, recheckFailed)
    recheckProblem = true
  }

  async function recheckNewer(revision: string): Promise<void> {
    await serially(async () => {
      if (!projection || BigInt(revision) <= BigInt(projection.revision)) return
      await reportedRecheck(undefined, true)
    })
  }

  async function reloadDay(): Promise<void> {
    await serially(async () => {
      if (!projection) return
      const current = projection
      const body = viewBody(current.canonical_query, current.selection, alignedOffset(cursorIndex))
      await requestProjection(
        () => client.view(body, activeRequest?.signal),
        (next) => {
          // The windows read before hold the earlier day's rows under the same revision.
          cache.clear()
          acceptProfileProjection(next, true)
        },
        reloadDay,
      )
    })
  }

  async function apply(action: TransitionAction): Promise<boolean> {
    return await serially(() => transitionNow(action))
  }

  // Each transition starts from the view the one before it returned, whichever control sent
  // it, so rapid clicks step one at a time instead of aborting each other. Chart moves and
  // rechecks wait their turn too, so they cannot abort a transition still on its way.
  async function serially<T>(work: () => Promise<T>): Promise<T> {
    const result = applyTail.then(async () => {
      await barrier
      return await work()
    })
    applyTail = result.then(
      () => undefined,
      () => undefined,
    )
    return await result
  }

  async function transitionNow(
    action: TransitionAction,
    historyMode?: 'replace',
  ): Promise<boolean> {
    if (!projection) return false
    lateReturn = undefined
    const prior = projection
    const fromTop = startsAtFirstRow(action.action)
    const body: TransitionBody = {
      query: prior.canonical_query,
      selection: prior.selection,
      action: action.action,
      window: { offset: fromTop ? 0 : alignedOffset(cursorIndex), limit: windowSize },
      ...(action.target === undefined ? {} : { target: action.target }),
      ...(action.search === undefined ? {} : { search: action.search }),
      ...(action.filters === undefined ? {} : { filters: withPeriod(action.filters, prior) }),
      ...(action.dimension === undefined ? {} : { dimension: action.dimension }),
      ...(action.currency === undefined ? {} : { currency: action.currency }),
      ...(action.sort_field === undefined ? {} : { sort_field: action.sort_field }),
      ...(action.period === undefined ? {} : { period: action.period }),
      ...(action.depth === undefined ? {} : { depth: action.depth }),
    }
    const run = async () => {
      return await requestProjection(
        async () => ensureVisibleWindow(await client.transition(body, activeRequest?.signal)),
        async (next) => {
          if (action.action === 'view.back' || action.action === 'view.back-to') {
            await returnTo(next)
            return
          }
          const selectionOnly = action.action.startsWith('selection.')
          if (fromTop) moveToFirstRow()
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
    const current = ledger.read(viewState.state)
    const delta = ledger.deltaTo(next.canonical_query, viewState.state)
    if (current?.query === next.canonical_query || current === undefined || delta === undefined) {
      accept(next, 'replace')
      return Promise.resolve()
    }
    const traversal = new Promise<void>((resolve) => {
      const timer = setTimeout(() => {
        if (returning?.projection !== next) return
        lateReturn = returning
        returning = undefined
        if (!newerThan(next)) accept(next, 'none')
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
    flushHistory()
    viewState.go(delta)
    return traversal
  }

  async function installReturn(
    returned: ReturnInProgress,
    owned: MoneyflowHistoryState,
  ): Promise<void> {
    generation += 1
    activeRequest?.abort()
    loading = false
    // Later transitions wait for this return and its cursor window, so a row key targets the
    // restored cursor row. They must run even if installing it fails. The saved cursor can lie
    // past the end once an edit shrank the parent, so the window follows the clamped cursor.
    try {
      accept(returned.projection, 'replace', owned)
      if (alignedOffset(cursorIndex) !== returned.projection.window.offset) {
        await moveToIndex(cursorIndex)
      }
    } finally {
      returned.settle()
    }
  }

  async function restore(event: Pick<PopStateEvent, 'state'>): Promise<void> {
    // The browser already left the entry the pending cursor belongs to; keep it for a return.
    const left = dropPendingHistory()
    if (left) leftCursors.set(left.state.sequence, left.state)
    searchSnapshot = undefined
    const owned = withLeftCursor(ledger.record(event.state))
    // A late traversal's view is current only until anything else replaces it, such as an edit.
    const late = lateReturn?.projection === projection ? lateReturn : undefined
    const returned = returning ?? late
    returning = undefined
    lateReturn = undefined
    const arrived = owned && returned?.sequence === owned.sequence ? returned : undefined
    if (arrived && owned && !newerThan(arrived.projection)) {
      await installReturn(arrived, owned)
      return
    }
    // Fetch the view instead. When an edit made the returned view stale, use the selection Esc
    // left, not the older one history saved. Keys wait until the view loads.
    const query = owned?.query ?? viewState.query
    const selection = arrived?.projection.selection ?? owned?.selection ?? projection?.selection
    let loaded = false
    try {
      loaded = await requestProjection(
        async () =>
          ensureVisibleWindow(
            await client.view(
              viewBody(query, selection, alignedOffset(owned?.cursorIndex ?? 0)),
              activeRequest?.signal,
            ),
          ),
        (next) => accept(next, 'replace', owned),
        () => restore(event),
      )
    } finally {
      returned?.settle()
    }
    // As on a first load: a workspace that comes back from setup restores its view, and setup
    // can have imported the profile's first transactions.
    if (loaded) await openInBusiestCurrency()
  }

  async function retry(): Promise<void> {
    await lastRetry?.()
  }

  async function reset(): Promise<void> {
    dropPendingHistory()
    leftCursors.clear()
    viewState.replace(null, '')
    await hydrate()
  }

  async function transactionInformation(
    identity: string,
  ): Promise<TransactionInformationResponse | undefined> {
    if (!projection) return undefined
    try {
      if (!client.transactionInformation) return undefined
      return await client.transactionInformation({
        version: '1',
        expected_revision: projection.revision,
        query: projection.canonical_query,
        target: { kind: 'transaction', identity },
        match_window: { offset: 0, limit: 20 },
        item_window: { offset: 0, limit: 20 },
      })
    } catch (error) {
      if (stopped) return undefined
      announcement = `Transaction information could not be loaded. ${failureReason(error)}`
      showFailure(announcement)
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
    }
    return searchSnapshot
  }

  async function previewSearch(search: string): Promise<SearchPreview> {
    if (!projection) return 'superseded'
    const query = searchSnapshot?.projection.canonical_query ?? projection.canonical_query
    const selection = searchSnapshot?.projection.selection ?? projection.selection
    let accepted = false
    const request = requestProjection(
      async () => {
        try {
          return ensureVisibleWindow(
            await client.transition(
              {
                query,
                selection,
                action: 'search.apply',
                search,
                window: { offset: 0, limit: windowSize },
              },
              activeRequest?.signal,
            ),
          )
        } catch (error) {
          if (!isNoChange(error)) throw error
          // The search asks for the view search opened on, such as after clearing the field.
          return await client.view(viewBody(query, selection, 0), activeRequest?.signal)
        }
      },
      (next) => {
        moveToFirstRow()
        accept(next, 'replace')
        accepted = true
      },
      () => previewSearch(search).then(() => undefined),
      // The search field shows why a preview failed; a banner per keystroke would bury it.
      false,
    )
    const requestGeneration = generation
    await request
    if (accepted) return 'applied'
    if (requestGeneration !== generation || !problem) return 'superseded'
    return invalidSearchCodes.has(problem.code) ? 'invalid' : { failed: problem.detail }
  }

  function commitSearch(snapshot: SearchSnapshot): void {
    if (!projection || snapshot !== searchSnapshot) return
    const accepted = projection
    flushHistory()
    viewState.replace(snapshot.history, snapshot.projection.canonical_query)
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
    problem = undefined
    announcement = snapshot.projection.status ?? ''
    sequence = snapshot.history.sequence
    cache.store(snapshot.projection)
    editing.sync(snapshot.projection)
    provider.sync(snapshot.projection)
    flushHistory()
    viewState.replace(snapshot.history, snapshot.projection.canonical_query)
    ledger.record(snapshot.history)
    searchSnapshot = undefined
  }

  async function moveCursor(delta: -1 | 1): Promise<void> {
    if (!projection || projection.total_rows === 0) return
    const nextIndex = Math.min(Math.max(cursorIndex + delta, 0), projection.total_rows - 1)
    if (nextIndex === cursorIndex) return keepCursor()
    await moveToIndex(nextIndex)
  }

  async function moveHome(): Promise<void> {
    if (!projection || projection.total_rows === 0) return
    if (cursorIndex !== 0) return moveToIndex(0)
    keepCursor()
  }

  /** A key left the cursor on its row, so a window that scrolling asked for must not move it. */
  function keepCursor(): void {
    showing?.abort.abort()
    showing = undefined
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
      deferOwnedHistory()
      schedulePrefetch(cached)
      return
    }
    const query = projection.canonical_query
    const selection = projection.selection
    await requestProjection(
      () => client.view(viewBody(query, selection, offset), activeRequest?.signal),
      (next) => {
        setProjectionWindow(next, nextIndex)
        deferOwnedHistory()
        schedulePrefetch(next)
      },
      () => moveToIndex(nextIndex),
    )
  }

  async function showIndex(index: number): Promise<void> {
    if (!projection || !Number.isSafeInteger(index)) return
    if (index < 0 || index >= projection.total_rows) return
    const offset = alignedOffset(index)
    // A row of the loaded window: the user scrolled back, so a window on its way is not needed.
    if (offset === projection.window.offset) return void showing?.abort.abort()
    if (showing?.offset === offset && !showing.superseded()) {
      showing.index = index
      return
    }
    showing?.abort.abort()
    showing = undefined
    const current = projection
    const cached = cache.get(current.canonical_query, current.selection, current.revision, offset)
    if (cached) return showWindow(cached, index)
    // A projection change or cursor move after this request starts is newer, so it wins.
    const requestGeneration = generation
    const requestCursor = cursorIndex
    const request: ShowRequest = {
      offset,
      index,
      abort: new AbortController(),
      superseded: () =>
        request.abort.signal.aborted ||
        projection !== current ||
        generation !== requestGeneration ||
        cursorIndex !== requestCursor,
    }
    showing = request
    try {
      const next = await client.view(
        viewBody(current.canonical_query, current.selection, offset),
        request.abort.signal,
      )
      if (request.superseded()) return
      showWindow(next, request.index)
    } catch (error) {
      if (isAbort(error) || request.superseded()) return
      lastRetry = () => showIndex(request.index)
      setProblem(error, changeFailed)
    } finally {
      if (showing === request) showing = undefined
    }
  }

  async function showAnchor(identity: string): Promise<void> {
    await serially(() => anchorNow(identity))
  }

  async function anchorNow(identity: string): Promise<void> {
    if (!projection) return
    const current = projection
    // A cursor move in a cached window bumps no generation, but it is newer, so it wins.
    const requestCursor = cursorIndex
    const body = {
      ...viewBody(current.canonical_query, current.selection, alignedOffset(requestCursor)),
      anchor: identity,
    }
    await requestProjection(
      () => client.view(body, activeRequest?.signal),
      (next) => {
        if (projection !== current || cursorIndex !== requestCursor) return
        if (next.anchor_index === undefined) return
        setProjectionWindow(next, next.anchor_index)
        replaceOwnedHistory()
        schedulePrefetch(next)
      },
      () => showAnchor(identity),
    )
  }

  function showWindow(next: ViewProjection, index: number): void {
    const { offset, count } = next.window
    const holdsCursor = cursorIndex >= offset && cursorIndex < offset + count
    problem = undefined
    setProjectionWindow(next, holdsCursor ? cursorIndex : index)
    deferOwnedHistory()
    schedulePrefetch(next)
  }

  async function requestProjection(
    request: () => Promise<ViewProjection>,
    onSuccess: (next: ViewProjection) => void | Promise<void>,
    retry: () => Promise<void>,
    report = true,
  ): Promise<boolean> {
    const work = foregroundProjection(request, onSuccess, retry, report)
    foregroundRequest = work
    const outcome = await work
    // The view is already what the user asked for. A recheck confirms it is current: success
    // clears an earlier failed recheck, and failure shows as one, with its own Retry.
    if (outcome === 'unchanged') await reportedRecheck(undefined, true)
    return outcome === 'applied'
  }

  async function foregroundProjection(
    request: () => Promise<ViewProjection>,
    onSuccess: (next: ViewProjection) => void | Promise<void>,
    retry: () => Promise<void>,
    report: boolean,
  ): Promise<'applied' | 'unchanged' | 'not-applied'> {
    // Every transition and reload passes here, queued or not. A stopped controller sends none:
    // nothing shows its view, and a failure would show on the page that replaced it.
    if (stopped) return 'not-applied'
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
      if (requestGeneration !== generation) return 'not-applied'
      await onSuccess(next)
      return 'applied'
    } catch (error) {
      if (requestGeneration !== generation || isAbort(error)) return 'not-applied'
      if (isNoChange(error)) return 'unchanged'
      setProblem(error, changeFailed, report)
      return 'not-applied'
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
    announcement = next.warnings?.[0]?.detail ?? next.status ?? ''
    problem = undefined
    showWarnings(next)

    if (historyMode === 'none') {
      schedulePrefetch(next)
      return
    }
    flushHistory()
    if (historyMode === 'push') {
      sequence += 1
      forgetLeftCursorsFrom(sequence)
    }
    const state = ownedState(next)
    if (historyMode === 'push') viewState.push(state, next.canonical_query)
    else viewState.replace(state, next.canonical_query)
    ledger.record(state)
    schedulePrefetch(next)
  }

  /** Whether the view shown has a newer profile revision than `cached`, such as after an edit. */
  function newerThan(cached: ViewProjection): boolean {
    return projection !== undefined && BigInt(cached.revision) < BigInt(projection.revision)
  }

  function acceptProfileProjection(
    next: ViewProjection,
    replaceHistory = false,
    keepProblem = false,
  ): boolean {
    if (newerThan(next)) return false
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
    if (!keepProblem) {
      announcement = next.warnings?.[0]?.detail ?? next.status ?? ''
      problem = undefined
    }
    showWarnings(next)
    if (replaceHistory) replaceOwnedHistory()
    schedulePrefetch(next)
    void openInBusiestCurrency()
    return true
  }

  /** Forgets the cursor's row, so the projection accepted next puts the cursor on its first row. */
  function moveToFirstRow(): void {
    cursorIdentity = undefined
    cursorIndex = 0
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
    dropPendingHistory()
    const state = ownedState(projection)
    viewState.replace(state, projection.canonical_query)
    ledger.record(state)
  }

  /** Records the cursor in the current entry once moves pause; see historyDebounceMillis. */
  function deferOwnedHistory(): void {
    if (!projection) return
    dropPendingHistory()
    pendingHistory = { state: ownedState(projection), query: projection.canonical_query }
    if (historyDebounceMillis <= 0) flushHistory()
    else pendingHistoryTimer = setTimeout(flushHistory, historyDebounceMillis)
  }

  function flushHistory(): void {
    const pending = dropPendingHistory()
    if (!pending) return
    // A host can move to an entry of its own before the pause ends; keep the cursor for a return.
    if (ledger.read(viewState.state)?.sequence !== pending.state.sequence) {
      leftCursors.set(pending.state.sequence, pending.state)
      return
    }
    viewState.replace(pending.state, pending.query)
    ledger.record(pending.state)
  }

  function stop(): void {
    stopped = true
    generation += 1
    activeRequest?.abort()
    // The request this cancels no longer owns `loading`, so it would never clear it, and every
    // wait for it would spin.
    loading = false
    provider.stop()
    providerWrite.stop()
    editing.stop()
    duplicates.stop()
    // Closing export cancels a preview or download on its way, so it neither fails nor saves.
    exportController.close()
  }

  // A pending cursor is always the newest state of its entry: anything else that writes the
  // entry writes the pending cursor first or drops it.
  function withLeftCursor(
    owned: MoneyflowHistoryState | undefined,
  ): MoneyflowHistoryState | undefined {
    const left = owned && leftCursors.get(owned.sequence)
    if (!owned || !left || left.query !== owned.query) return owned
    leftCursors.delete(owned.sequence)
    return left
  }

  // A push replaces every entry from its sequence forward, so their left cursors are gone too.
  function forgetLeftCursorsFrom(first: number): void {
    for (const entry of leftCursors.keys()) {
      if (entry >= first) leftCursors.delete(entry)
    }
  }

  function dropPendingHistory(): typeof pendingHistory {
    clearTimeout(pendingHistoryTimer)
    pendingHistoryTimer = undefined
    const pending = pendingHistory
    pendingHistory = undefined
    return pending
  }

  function ownedState(next: ViewProjection): MoneyflowHistoryState {
    return {
      owner: 'moneyflow-web-v1',
      instance,
      sequence,
      query: next.canonical_query,
      ...(cursorIdentity === undefined ? {} : { cursorIdentity }),
      cursorIndex,
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
      void client
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
          // Scrolling may have moved the loaded window since this prefetch started.
          cache.retainAdjacent(
            current.canonical_query,
            current.selection,
            current.revision,
            projection.window.offset,
          )
          cacheChanges += 1
        })
        .catch(() => undefined)
        .finally(() => prefetchRequests.delete(key))
    }
  }

  async function ensureVisibleWindow(next: ViewProjection): Promise<ViewProjection> {
    if (next.total_rows === 0 || next.window.count > 0 || next.window.offset === 0) return next
    return await client.view(
      viewBody(next.canonical_query, next.selection, alignedOffset(next.total_rows - 1)),
      activeRequest?.signal,
    )
  }

  /**
   * Records why a request failed. Once a view is on screen, a banner says so too; before that,
   * the page that replaces the workspace does.
   */
  function setProblem(error: unknown, failure: string, report = true): void {
    problemsRecorded += 1
    recordProblem(error, failure, report)
  }

  function recordProblem(error: unknown, failure: string, report = true): void {
    problem = controllerProblem(error)
    recheckProblem = false
    announcement =
      problem.kind === 'invalid-view'
        ? 'This Moneyflow link is invalid.'
        : `${failure} ${failureReason(error)}`
    if (report && projection) showFailure(announcement)
  }

  return {
    get projection() {
      return projection
    },
    get loading() {
      return loading
    },
    get opening() {
      return opening
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
    get neighbors() {
      void cacheChanges
      if (!projection) return []
      const { canonical_query, selection, revision, window } = projection
      return [window.offset - windowSize, window.offset + windowSize].flatMap(
        (offset) => cache.get(canonical_query, selection, revision, offset) ?? [],
      )
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
    viewState,
    hydrate,
    recheck,
    moveCursor,
    moveCursorTo,
    moveHome,
    showIndex,
    showAnchor,
    recheckNewer,
    reloadDay,
    insights: (body, signal) => client.insights(body, signal),
    apply,
    hold,
    settled,
    beginSearch,
    previewSearch,
    commitSearch,
    restoreSearch,
    restore,
    flushHistory,
    stop,
    retry,
    reset,
    transactionInformation,
  }
}

/** Shows the warnings of a view the user now sees, except the one editing reports itself. */
function showWarnings(next: ViewProjection): void {
  const shown = (next.warnings ?? []).filter((warning) => warning.code !== auditWarningCode)
  if (shown.length) showNotice(shown.map((warning) => warning.detail).join(' '))
}

/** The codes the server answers a search expression it cannot parse with. */
const invalidSearchCodes = new Set(['invalid_web_request', 'invalid_request'])

function controllerProblem(error: unknown): ControllerProblem {
  if (error instanceof MoneyflowProblem) {
    const invalid = error.problem.code === 'invalid_view_state'
    return {
      kind: invalid ? 'invalid-view' : 'request',
      code: error.problem.code,
      detail: problemReason(error.problem),
    }
  }
  if (error instanceof TypeError) {
    return { kind: 'request', code: 'network_error', detail: NETWORK_FAILURE }
  }
  return { kind: 'request', code: 'invalid_response', detail: INVALID_RESPONSE }
}

/** Why a request failed: the server's reason, or what went wrong without one. */
function failureReason(error: unknown): string {
  return controllerProblem(error).detail
}

function viewBody(query: string, selection: SelectionValue | undefined, offset: number): ViewBody {
  return {
    query,
    ...(selection === undefined ? {} : { selection }),
    window: { offset, limit: windowSize },
  }
}

// filters.apply replaces the date range too, so it carries the period of the view it changes.
function withPeriod(
  filters: NonNullable<TransitionAction['filters']>,
  prior: ViewProjection,
): NonNullable<TransitionBody['filters']> {
  const range = prior.filters.date_range
  return { ...(range ? { date_range: { start: range.from, end: range.to } } : {}), ...filters }
}

function busiestCurrency(next: ViewProjection): TransitionAction['currency'] {
  if (next.view.currency) return undefined
  let busiest: ViewProjection['currencies'][number] | undefined
  for (const candidate of next.currencies) {
    if (busiest === undefined || candidate.count > busiest.count) busiest = candidate
  }
  return busiest && { currency: busiest.currency, scale: busiest.scale }
}

function alignedOffset(index: number): number {
  return Math.floor(Math.max(0, index) / windowSize) * windowSize
}

/** The server answers `no_change` when the view already is what the request asks for. */
function isNoChange(error: unknown): boolean {
  return error instanceof MoneyflowProblem && error.problem.code === 'no_change'
}

function isAbort(error: unknown): boolean {
  return (
    (error instanceof DOMException && error.name === 'AbortError') ||
    (typeof error === 'object' && error !== null && 'name' in error && error.name === 'AbortError')
  )
}
