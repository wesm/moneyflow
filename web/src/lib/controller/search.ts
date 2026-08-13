import { debounce } from '@kenn-io/kit-ui'

const maxSearchBytes = 2048
const previewDelay = 150

export interface SearchHost<Snapshot> {
  beginSearch(): Snapshot
  previewSearch(search: string): Promise<boolean>
  commitSearch(snapshot: Snapshot): void
  restoreSearch(snapshot: Snapshot): void
}

export interface SearchCoordinator {
  readonly value: string
  readonly error: string
  readonly pending: boolean
  input(value: string): void
  commit(): Promise<boolean>
  cancel(): void
  subscribe(listener: () => void): () => void
  destroy(): void
}

export function createSearchCoordinator<Snapshot>(
  host: SearchHost<Snapshot>,
  initialValue = '',
): SearchCoordinator {
  const snapshot = host.beginSearch()
  let value = initialValue
  let accepted = initialValue
  let error = ''
  let pending = false
  let generation = 0
  let activePreview: Promise<boolean> | undefined
  const listeners = new Set<() => void>()

  function notify(): void {
    for (const listener of listeners) listener()
  }

  const schedule = debounce(() => {
    activePreview = preview(value)
  }, previewDelay)

  function input(next: string): void {
    value = next
    error = ''
    schedule.cancel()
    generation += 1
    if (new TextEncoder().encode(next).byteLength > maxSearchBytes) {
      error = 'Search text must be at most 2048 bytes.'
      notify()
      return
    }
    notify()
    schedule()
  }

  async function preview(next: string): Promise<boolean> {
    const requestGeneration = ++generation
    pending = true
    notify()
    const valid = await host.previewSearch(next)
    if (requestGeneration !== generation) return false
    pending = false
    if (!valid) {
      error = 'That search expression is invalid.'
      notify()
      return false
    }
    accepted = next
    error = ''
    notify()
    return true
  }

  async function commit(): Promise<boolean> {
    schedule.cancel()
    if (activePreview) {
      await activePreview
      activePreview = undefined
    }
    if (value !== accepted || error !== '') {
      if (new TextEncoder().encode(value).byteLength > maxSearchBytes) return false
      if (!(await preview(value))) return false
    }
    host.commitSearch(snapshot)
    return true
  }

  function cancel(): void {
    schedule.cancel()
    generation += 1
    pending = false
    notify()
    host.restoreSearch(snapshot)
  }

  return {
    get value() {
      return value
    },
    get error() {
      return error
    },
    get pending() {
      return pending
    },
    input,
    commit,
    cancel,
    subscribe: (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    destroy: () => {
      schedule.cancel()
      generation += 1
      listeners.clear()
    },
  }
}
