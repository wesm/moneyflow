import type { components } from '../api/schema'
import { SvelteURL } from 'svelte/reactivity'
import {
  createMutationFetch,
  MoneyflowProblem,
  requestProfileJSON,
  type MutationFetch,
} from '../api/client'

export type AmazonImportStatus = components['schemas']['AmazonImportStatusResponse']
export type AmazonImportCoordinate = components['schemas']['AmazonImportCoordinate']

export interface AmazonImportState {
  phase: 'settings' | 'source' | 'uploading' | 'importing' | 'complete' | 'failed' | 'canceled'
  snapshot?: AmazonImportStatus
  coordinate?: AmazonImportCoordinate
  announcement: string
  problem?: string
}

export interface AmazonImportController {
  readonly state: AmazonImportState
  start(currency: string, scale: number, taxonomySourceID?: string): Promise<boolean>
  upload(files: File[]): Promise<boolean>
  execute(): Promise<boolean>
  cancel(): Promise<void>
  destroy(): void
}

export interface AmazonImportTransport {
  start(body: components['schemas']['AmazonImportStartBody']): Promise<AmazonImportStatus>
  upload(attemptID: string, stateVersion: string, files: File[]): Promise<AmazonImportStatus>
  execute(attemptID: string, stateVersion: string): Promise<AmazonImportStatus>
  status(attemptID: string, signal?: AbortSignal): Promise<AmazonImportStatus>
  cancel(attemptID: string, stateVersion: string): Promise<AmazonImportStatus>
}

export function createAmazonImportController(options: {
  transport: AmazonImportTransport
}): AmazonImportController {
  let state = $state<AmazonImportState>({
    phase: 'settings',
    announcement: 'Confirm currency and minor-unit scale.',
  })
  let polling: number | undefined
  let destroyed = false

  function install(snapshot: AmazonImportStatus): void {
    if (destroyed) return
    const phase = phaseFor(snapshot)
    state = {
      phase,
      snapshot,
      announcement: announcementFor(snapshot),
      ...(snapshot.coordinate ? { coordinate: snapshot.coordinate } : {}),
      ...(phase === 'failed' ? { problem: failureMessage(snapshot) } : {}),
    }
  }

  async function start(currency: string, scale: number, taxonomySourceID = ''): Promise<boolean> {
    try {
      install(
        await options.transport.start({
          version: '1',
          currency: currency.trim().toUpperCase(),
          scale,
          ...(taxonomySourceID.trim() ? { taxonomy_source_id: taxonomySourceID.trim() } : {}),
        }),
      )
      return true
    } catch (error) {
      fail(error, 'Amazon import could not start.')
      return false
    }
  }

  async function upload(files: File[]): Promise<boolean> {
    const snapshot = state.snapshot
    if (!snapshot || files.length === 0) return false
    state = { ...state, phase: 'uploading', announcement: 'Uploading Amazon order history…' }
    try {
      install(await options.transport.upload(snapshot.attempt_id, snapshot.state_version, files))
      return true
    } catch (error) {
      fail(error, 'Amazon order-history files could not be staged.')
      return false
    }
  }

  async function execute(): Promise<boolean> {
    const snapshot = state.snapshot
    if (!snapshot) return false
    state = { ...state, phase: 'importing', announcement: 'Importing Amazon order history…' }
    const abort = new AbortController()
    polling = window.setInterval(() => {
      void options.transport
        .status(snapshot.attempt_id, abort.signal)
        .then(install)
        .catch(() => undefined)
    }, 500)
    try {
      install(await options.transport.execute(snapshot.attempt_id, snapshot.state_version))
      return state.phase === 'complete'
    } catch (error) {
      fail(error, 'Amazon order history could not be imported.')
      return false
    } finally {
      abort.abort()
      if (polling !== undefined) window.clearInterval(polling)
      polling = undefined
    }
  }

  async function cancel(): Promise<void> {
    const snapshot = state.snapshot
    if (!snapshot) {
      state = { ...state, phase: 'canceled', announcement: 'Amazon import canceled.' }
      return
    }
    try {
      install(await options.transport.cancel(snapshot.attempt_id, snapshot.state_version))
    } catch (error) {
      fail(error, 'Amazon import could not be canceled safely.')
    }
  }

  function fail(error: unknown, fallback: string): void {
    const detail = error instanceof MoneyflowProblem ? error.problem.detail : fallback
    state = { ...state, phase: 'failed', problem: detail, announcement: detail }
  }

  return {
    get state() {
      return state
    },
    start,
    upload,
    execute,
    cancel,
    destroy() {
      destroyed = true
      if (polling !== undefined) window.clearInterval(polling)
    },
  }
}

export function createAmazonImportTransport(
  basePath: string,
  profileID: string,
  upstream: typeof fetch = fetch,
  mutations: MutationFetch = createMutationFetch(
    basePath,
    upstream,
    null,
    Date.now,
    `api/v1/profiles/${profileID}/bootstrap`,
  ),
): AmazonImportTransport {
  const origin = globalThis.location?.origin ?? 'http://localhost'
  const profilePrefix = `api/v1/profiles/${profileID}/amazon-import/`
  const mutationPath = (suffix: string) => `${profilePrefix}${suffix}`
  return {
    start(body) {
      return requestProfileJSON(mutations, mutationPath('start'), body)
    },
    async upload(attemptID, stateVersion, files) {
      const form = new FormData()
      form.set('version', '1')
      form.set('expected_state_version', stateVersion)
      for (const file of files) form.append('files', file, relativeName(file))
      if (!mutations.upload) throw new Error('Amazon file upload is unavailable.')
      const response = await mutations.upload(mutationPath(`${attemptID}/files`), form)
      return responseValue(response)
    },
    execute(attemptID, stateVersion) {
      return requestProfileJSON(mutations, mutationPath(`${attemptID}/execute`), {
        version: '1',
        expected_state_version: stateVersion,
      })
    },
    async status(attemptID, signal) {
      const response = await upstream(
        new SvelteURL(`${profilePrefix}${attemptID}/status`, new SvelteURL(basePath, origin)),
        {
          method: 'GET',
          cache: 'no-store',
          credentials: 'omit',
          redirect: 'error',
          ...(signal === undefined ? {} : { signal }),
        },
      )
      return responseValue(response)
    },
    cancel(attemptID, stateVersion) {
      return requestProfileJSON(mutations, mutationPath(`${attemptID}/cancel`), {
        version: '1',
        expected_state_version: stateVersion,
      })
    },
  }
}

async function responseValue(response: Response): Promise<AmazonImportStatus> {
  const value: unknown = await response.json()
  if (!response.ok) {
    if (isProblem(value)) throw new MoneyflowProblem(value)
    throw new Error('The Amazon import response is invalid.')
  }
  if (!isStatus(value)) throw new Error('The Amazon import response is invalid.')
  return value
}

function phaseFor(snapshot: AmazonImportStatus): AmazonImportState['phase'] {
  if (snapshot.state === 'complete') return 'complete'
  if (snapshot.state === 'failed') return 'failed'
  if (snapshot.state === 'canceled') return 'canceled'
  if (snapshot.state === 'parsing' || snapshot.state === 'installing') return 'importing'
  return 'source'
}

function announcementFor(snapshot: AmazonImportStatus): string {
  if (snapshot.state === 'complete') {
    const changed = snapshot.result.inserted + snapshot.result.updated + snapshot.result.restored
    return snapshot.result.no_op
      ? 'Amazon order history is already current.'
      : `${changed.toLocaleString()} Amazon transaction${changed === 1 ? '' : 's'} imported.`
  }
  if (snapshot.state === 'failed') return failureMessage(snapshot)
  if (snapshot.state === 'canceled') return 'Amazon import canceled.'
  if (snapshot.state === 'parsing') {
    return `Parsing Amazon order history: ${snapshot.progress.completed.toLocaleString()} records.`
  }
  if (snapshot.state === 'installing') return 'Installing Amazon transactions atomically…'
  return 'Choose Amazon order-history CSV files.'
}

function failureMessage(snapshot: AmazonImportStatus): string {
  if (snapshot.coordinate) {
    const column = snapshot.coordinate.column ? `, ${snapshot.coordinate.column}` : ''
    return `${snapshot.coordinate.relative_filename}, record ${snapshot.coordinate.record}${column}: ${snapshot.coordinate.reason}`
  }
  return snapshot.failure_code || 'Amazon import failed.'
}

function relativeName(file: File): string {
  const candidate = (file as File & { webkitRelativePath?: string }).webkitRelativePath
  return candidate || file.name
}

function isStatus(value: unknown): value is AmazonImportStatus {
  return (
    isRecord(value) &&
    value.version === '1' &&
    typeof value.attempt_id === 'string' &&
    typeof value.state_version === 'string' &&
    typeof value.state === 'string' &&
    isRecord(value.result)
  )
}

function isProblem(value: unknown): value is components['schemas']['Problem'] {
  return (
    isRecord(value) &&
    typeof value.type === 'string' &&
    typeof value.title === 'string' &&
    typeof value.status === 'number' &&
    typeof value.detail === 'string' &&
    typeof value.code === 'string'
  )
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
