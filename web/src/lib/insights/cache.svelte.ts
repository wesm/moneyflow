import { untrack } from 'svelte'
import { SvelteMap } from 'svelte/reactivity'

import type { InsightsBody, InsightsResponse, InsightsSeries, ViewProjection } from '../api/client'
import { insightsBodies, insightsQuery, missingRows } from './requests'

const maxEntries = 8
/**
 * How long an answer vouches for the server's day. The day can change with no new revision, so
 * after this long the displayed view asks for its summary again, even when it is cached.
 */
const freshMillis = 5 * 60_000

/** Insights for one canonical query at one profile revision. */
export interface InsightsEntry {
  readonly query: string
  readonly revision: string
  /** The response that carried the summary; its `series` holds only the rows it was asked for. */
  readonly response: InsightsResponse
  /** Every row series loaded for this query and revision, by row identity. */
  readonly series: ReadonlyMap<string, InsightsSeries>
}

export type InsightsStatus = 'idle' | 'loading' | 'ready' | 'failed'

export type InsightsLoad = (body: InsightsBody, signal: AbortSignal) => Promise<InsightsResponse>

/** A cache slot; series can arrive before the summary. */
interface Slot {
  readonly query: string
  readonly revision: string
  readonly response: InsightsResponse | undefined
  readonly series: SvelteMap<string, InsightsSeries>
}

interface Flight {
  readonly key: string
  readonly query: string
  readonly revision: string
  /** The server's day when the request was sent. */
  readonly today: string | undefined
  readonly rows: readonly string[]
  readonly summary: boolean
  readonly abort: AbortController
}

interface DisplayedView {
  readonly key: string
  readonly query: string
  readonly revision: string
}

/**
 * Insights for the displayed view, kept for the eight most recent queries and revisions. The
 * query is `insightsQuery`'s, so views that differ only by sort share an entry. Charts
 * draw only the entry whose query and revision equal the displayed projection's; until it
 * arrives, `entry` stays on the previous one and `stale` is set.
 */
export class InsightsCache {
  readonly #load: InsightsLoad
  readonly #onNewer: (revision: string) => unknown
  readonly #slots = new SvelteMap<string, Slot>()
  #flights: Flight[] = []
  #view = $state.raw<DisplayedView | undefined>()
  #shown = $state.raw<InsightsEntry | undefined>()
  // Keys whose summary or series request failed; only `retry` or another key requests again.
  #summaryFailed = $state.raw<string | undefined>()
  #seriesFailed = $state.raw<string | undefined>()
  // The key whose newer insights wait on `onNewer`, which rechecks the view.
  #awaitingView: string | undefined
  #rows: readonly string[] = []
  #today: string | undefined
  /** When the last answer the cache kept arrived, from `Date.now()`. */
  #answeredAt = 0
  #notified = -1n
  #disposed = false

  /**
   * @param load Sends one insights request.
   * @param onNewer Called once per revision when insights report a newer revision than the
   *   displayed projection; the view must be rechecked before those insights can show. When it
   *   returns a promise that settles with the view unchanged, the missing summary or series
   *   count as failed, and a later answer at that revision, such as after `retry`, calls it again.
   */
  constructor(load: InsightsLoad, onNewer: (revision: string) => unknown) {
    this.#load = load
    this.#onNewer = onNewer
  }

  /** The entry to draw: the displayed view's, or the previous one while that loads. */
  get entry(): InsightsEntry | undefined {
    return this.#view === undefined ? undefined : this.#shown
  }

  /** Whether `entry` belongs to an earlier query or revision than the displayed view. */
  get stale(): boolean {
    const shown = this.entry
    return shown !== undefined && keyOf(shown.query, shown.revision) !== this.#view?.key
  }

  /** The state of the displayed view's summary; series failures are in `seriesFailed`. */
  get status(): InsightsStatus {
    if (this.#view === undefined) return 'idle'
    if (this.#summaryFailed === this.#view.key) return 'failed'
    return this.#slots.get(this.#view.key)?.response ? 'ready' : 'loading'
  }

  /** Whether a series request for the displayed view failed; `retry` requests it again. */
  get seriesFailed(): boolean {
    return this.#view !== undefined && this.#seriesFailed === this.#view.key
  }

  /**
   * The row's series for the displayed view. A stale entry of the same query still answers: the
   * insights read committed values, so a revision from a staged edit leaves the series unchanged.
   */
  seriesFor(identity: string): InsightsSeries | undefined {
    if (this.#view === undefined) return undefined
    const current = this.#slots.get(this.#view.key)?.series.get(identity)
    if (current !== undefined || this.#shown?.query !== this.#view.query) return current
    return this.#shown.series.get(identity)
  }

  /**
   * Shows `projection` and requests its summary and the series of `rows` that are neither loaded
   * nor on their way. After a failure, only `retry` or another view requests again. Reads no
   * state, so an effect that calls it reruns only for its own dependencies.
   */
  sync(projection: ViewProjection, rows: readonly string[]): void {
    untrack(() => {
      if (this.#disposed) return
      this.#rows = rows
      if (projection.view.currency === undefined) {
        this.#show(undefined)
        return
      }
      const { revision } = projection
      const query = insightsQuery(projection.canonical_query)
      const key = keyOf(query, revision)
      if (this.#view?.key !== key) this.#show({ key, query, revision })
      if (!this.#failedFor(key)) this.#request()
    })
  }

  /** Requests the displayed view's missing summary and series again after a failure. */
  retry(): void {
    untrack(() => {
      if (this.#disposed || this.#view === undefined || !this.#failedFor(this.#view.key)) return
      this.#summaryFailed = undefined
      this.#seriesFailed = undefined
      this.#request()
    })
  }

  /** Aborts every request and ignores every later call and response. */
  dispose(): void {
    this.#disposed = true
    this.#abortFlights(undefined)
  }

  #failedFor(key: string): boolean {
    return this.#summaryFailed === key || this.#seriesFailed === key
  }

  #show(view: DisplayedView | undefined): void {
    this.#view = view
    this.#summaryFailed = undefined
    this.#seriesFailed = undefined
    this.#abortFlights(view?.key)
    if (view) this.#touch(view.key)
    this.#refreshShown()
  }

  /** Aborts the requests for every key but `kept`. */
  #abortFlights(kept: string | undefined): void {
    for (const flight of this.#flights) {
      if (flight.key !== kept) flight.abort.abort()
    }
    this.#flights = this.#flights.filter((flight) => flight.key === kept)
  }

  #request(): void {
    const view = this.#view
    if (view === undefined) return
    const slot = this.#slots.get(view.key)
    const pending = this.#flights.filter((flight) => flight.key === view.key)
    const missing = missingRows(
      this.#rows,
      slot?.series,
      pending.map((flight) => flight.rows),
    )
    const due = slot?.response === undefined || Date.now() - this.#answeredAt >= freshMillis
    const summary = due && !pending.some((flight) => flight.summary)
    for (const body of insightsBodies(view.query, missing, summary)) this.#send(view, body)
  }

  #send(view: DisplayedView, body: InsightsBody): void {
    const flight: Flight = {
      ...view,
      today: this.#today,
      rows: body.rows ?? [],
      summary: body.summary === true,
      abort: new AbortController(),
    }
    this.#flights.push(flight)
    const settle = () => {
      this.#flights = this.#flights.filter((candidate) => candidate !== flight)
    }
    this.#load(body, flight.abort.signal).then(
      (response) => {
        settle()
        if (flight.abort.signal.aborted) return
        this.#receive(flight, response)
        this.#checkWaiting()
      },
      () => {
        settle()
        if (flight.abort.signal.aborted) return
        if (flight.summary) this.#summaryFailed = flight.key
        else this.#seriesFailed = flight.key
      },
    )
  }

  #receive(flight: Flight, response: InsightsResponse): void {
    const revision = BigInt(response.revision)
    const requested = BigInt(flight.revision)
    if (revision < requested) return
    // ISO dates order as strings. A response from an earlier day is dropped. One sent before the
    // day changed is asked again; one sent after means the server's day went back, so it fails.
    if (this.#today !== undefined && response.today < this.#today) {
      if (flight.today !== this.#today) this.#request()
      return
    }
    const dayChanged = this.#today !== undefined && response.today !== this.#today
    this.#today = response.today
    this.#answeredAt = Date.now()
    if (dayChanged) this.#clear()
    this.#store(flight.query, response)
    const newer = revision > requested
    // The chart context ends at today, so the displayed view loads again for the new day. Newer
    // insights move the view to another key first, so the old key is not asked again.
    if (dayChanged && !newer) this.#request()
    if (newer && revision > this.#notified) this.#notifyNewer(flight.key, response)
  }

  #notifyNewer(key: string, response: InsightsResponse): void {
    const revision = BigInt(response.revision)
    this.#notified = revision
    this.#awaitingView = key
    const settled = () => {
      // A recheck that failed, or whose view another move replaced, left the view behind, so a
      // later answer at this revision may report it again.
      if (this.#view?.key === key && this.#notified === revision) this.#notified = -1n
      if (this.#awaitingView === key) this.#awaitingView = undefined
      this.#checkWaiting()
    }
    void Promise.resolve()
      .then(() => (this.#disposed ? undefined : this.#onNewer(response.revision)))
      .then(settled, settled)
  }

  /**
   * Marks the summary or series failed when the displayed view lacks them and nothing will bring
   * them, such as after a dropped response or a recheck that left the view behind, so `retry`
   * can ask again instead of `loading` lasting forever.
   */
  #checkWaiting(): void {
    const view = this.#view
    if (this.#disposed || view === undefined || this.#failedFor(view.key)) return
    if (this.#awaitingView === view.key) return
    if (this.#flights.some((flight) => flight.key === view.key)) return
    const slot = this.#slots.get(view.key)
    if (slot?.response === undefined) this.#summaryFailed = view.key
    else if (this.#rows.some((row) => !slot.series.has(row))) this.#seriesFailed = view.key
  }

  #store(query: string, response: InsightsResponse): void {
    const key = keyOf(query, response.revision)
    let slot = this.#slots.get(key)
    if (slot === undefined) {
      slot = { query, revision: response.revision, response: undefined, series: new SvelteMap() }
      this.#slots.set(key, slot)
    }
    if (response.summary !== undefined && slot.response === undefined) {
      slot = { ...slot, response }
      this.#slots.set(key, slot)
    }
    for (const row of response.series) slot.series.set(row.identity, row)
    this.#touch(key)
    this.#refreshShown()
  }

  /** Drops every entry, and the requests whose answers would belong to the earlier day. */
  #clear(): void {
    this.#abortFlights(undefined)
    this.#slots.clear()
    this.#shown = undefined
  }

  /** Marks `key` most recently used and drops the least recently used past eight. */
  #touch(key: string): void {
    const slot = this.#slots.get(key)
    if (slot === undefined) return
    this.#slots.delete(key)
    this.#slots.set(key, slot)
    for (const oldest of this.#slots.keys()) {
      if (this.#slots.size <= maxEntries) break
      this.#slots.delete(oldest)
    }
  }

  #refreshShown(): void {
    const slot = this.#view && this.#slots.get(this.#view.key)
    if (slot !== undefined && isEntry(slot) && slot !== this.#shown) this.#shown = slot
  }
}

function isEntry(slot: Slot): slot is Slot & InsightsEntry {
  return slot.response !== undefined
}

function keyOf(query: string, revision: string): string {
  return `${query}\u0000${revision}`
}
