import { untrack } from 'svelte'
import { SvelteMap } from 'svelte/reactivity'

import type { InsightsBody, InsightsResponse, InsightsSeries, ViewProjection } from '../api/client'
import { insightsBodies, missingRows } from './requests'

const maxEntries = 8

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
 * Insights for the displayed view, kept for the eight most recent queries and revisions. Charts
 * draw only the entry whose query and revision equal the displayed projection's; until it
 * arrives, `entry` stays on the previous one and `stale` is set.
 */
export class InsightsCache {
  readonly #load: InsightsLoad
  readonly #onNewer: (revision: string) => void
  readonly #slots = new SvelteMap<string, Slot>()
  #flights: Flight[] = []
  #view = $state.raw<DisplayedView | undefined>()
  #shown = $state.raw<InsightsEntry | undefined>()
  #failed = $state.raw<string | undefined>()
  #rows: readonly string[] = []
  #today: string | undefined
  #notified = -1n

  /**
   * @param load Sends one insights request.
   * @param onNewer Called once per revision when insights report a newer revision than the
   *   displayed projection; the view must be rechecked before those insights can show.
   */
  constructor(load: InsightsLoad, onNewer: (revision: string) => void) {
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

  get status(): InsightsStatus {
    if (this.#view === undefined) return 'idle'
    if (this.#failed === this.#view.key) return 'failed'
    return this.#slots.get(this.#view.key)?.response ? 'ready' : 'loading'
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
      this.#rows = rows
      if (projection.view.currency === undefined) {
        this.#show(undefined)
        return
      }
      const { canonical_query: query, revision } = projection
      const key = keyOf(query, revision)
      if (this.#view?.key !== key) this.#show({ key, query, revision })
      if (this.#failed !== key) this.#request()
    })
  }

  /** Requests the displayed view's missing insights again after a failure. */
  retry(): void {
    untrack(() => {
      if (this.#view === undefined || this.#failed !== this.#view.key) return
      this.#failed = undefined
      this.#request()
    })
  }

  #show(view: DisplayedView | undefined): void {
    this.#view = view
    this.#failed = undefined
    for (const flight of this.#flights) {
      if (flight.key !== view?.key) flight.abort.abort()
    }
    this.#flights = this.#flights.filter((flight) => flight.key === view?.key)
    if (view) this.#touch(view.key)
    this.#refreshShown()
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
    const summary = slot?.response === undefined && !pending.some((flight) => flight.summary)
    for (const body of insightsBodies(view.query, missing, summary)) this.#send(view, body)
  }

  #send(view: DisplayedView, body: InsightsBody): void {
    const flight: Flight = {
      ...view,
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
        if (!flight.abort.signal.aborted) this.#receive(flight, response)
      },
      () => {
        settle()
        if (!flight.abort.signal.aborted) this.#failed = flight.key
      },
    )
  }

  #receive(flight: Flight, response: InsightsResponse): void {
    const revision = BigInt(response.revision)
    const requested = BigInt(flight.revision)
    if (revision < requested) return
    if (response.today !== this.#today) {
      if (this.#today !== undefined) this.#clear()
      this.#today = response.today
    }
    this.#store(flight.query, response)
    if (revision > requested && revision > this.#notified) {
      this.#notified = revision
      this.#onNewer(response.revision)
    }
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

  #clear(): void {
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
