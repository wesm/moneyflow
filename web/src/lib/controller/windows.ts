import type { ViewProjection } from '../api/client'

export class WindowCache {
  readonly #windowSize: number
  readonly #entries = new Map<string, Map<number, ViewProjection>>()

  constructor(windowSize: number) {
    if (!Number.isSafeInteger(windowSize) || windowSize <= 0) {
      throw new Error('window size must be a positive integer')
    }
    this.#windowSize = windowSize
  }

  store(projection: ViewProjection): void {
    const offset = projection.window.offset
    if (offset % this.#windowSize !== 0) throw new Error('projection window is not aligned')
    const cacheKey = key(projection.canonical_query, projection.selection, projection.revision)
    let query = this.#entries.get(cacheKey)
    if (!query) {
      query = new Map()
      this.#entries.set(cacheKey, query)
    }
    query.set(offset, projection)
  }

  get(
    query: string,
    selection: string,
    revision: string,
    offset: number,
  ): ViewProjection | undefined {
    return this.#entries.get(key(query, selection, revision))?.get(offset)
  }

  offsets(query: string, selection: string, revision: string): number[] {
    return [...(this.#entries.get(key(query, selection, revision))?.keys() ?? [])].sort(
      (left, right) => left - right,
    )
  }

  retainAdjacent(query: string, selection: string, revision: string, currentOffset: number): void {
    const cacheKey = key(query, selection, revision)
    const entries = this.#entries.get(cacheKey)
    if (!entries) return
    const retained = new Set([
      Math.max(0, currentOffset - this.#windowSize),
      currentOffset,
      currentOffset + this.#windowSize,
    ])
    for (const offset of entries.keys()) {
      if (!retained.has(offset)) entries.delete(offset)
    }
    for (const candidate of this.#entries.keys()) {
      if (candidate !== cacheKey) this.#entries.delete(candidate)
    }
  }
}

function key(query: string, selection: string, revision: string): string {
  return `${query}\u0000${selection}\u0000${revision}`
}

export function preserveCursor(
  projection: ViewProjection,
  identity: string | undefined,
  absoluteIndex: number,
): { identity: string | undefined; index: number } {
  if (projection.total_rows === 0) return { identity: undefined, index: 0 }
  const rows = projection.detail_rows ?? projection.aggregate_rows ?? []
  const matching =
    identity === undefined ? undefined : rows.find((row) => row.identity === identity)
  if (matching) return { identity: matching.identity, index: matching.index }
  const index = Math.min(Math.max(absoluteIndex, 0), projection.total_rows - 1)
  const row = rows.find((candidate) => candidate.index === index)
  return { identity: row?.identity, index }
}
