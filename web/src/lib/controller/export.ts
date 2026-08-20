import { createSubscriber } from 'svelte/reactivity'

import type { ExportBody, ExportPreviewResponse, MoneyflowClient } from '../api/client'

export type ExportFormat = ExportBody['format']
export type ExportScope = ExportBody['scope']
export type ExportPhase = 'idle' | 'previewing' | 'ready' | 'exporting' | 'complete' | 'failed'

export interface ExportState {
  phase: ExportPhase
  format: ExportFormat
  scope: ExportScope
  count: number
  announcement: string
  preview: ExportPreviewResponse | undefined
  filename: string | undefined
  canCancel: boolean
}

export interface ExportController {
  readonly state: ExportState
  open(query: string): Promise<boolean>
  close(): void
  setFormat(format: ExportFormat): void
  setScope(scope: ExportScope): void
  export(): Promise<boolean>
  cancel(): void
}

interface ObjectURLs {
  createObjectURL(blob: Blob): string
  revokeObjectURL(url: string): void
}

interface ExportControllerOptions {
  client: Pick<MoneyflowClient, 'previewExport' | 'downloadExport'>
  root?: Document
  objectURLs?: ObjectURLs
}

const exportVersion = '2'

export function createExportController(options: ExportControllerOptions): ExportController {
  const root = options.root ?? document
  const objectURLs = options.objectURLs ?? URL
  let query = ''
  let request: AbortController | undefined
  let cancelled = false
  let state: ExportState = initialState()
  let notify = (): void => undefined
  const subscribe = createSubscriber((update) => {
    notify = update
    return () => {
      notify = () => undefined
    }
  })

  function setState(next: ExportState): void {
    state = next
    notify()
  }

  async function open(nextQuery: string): Promise<boolean> {
    request?.abort()
    request = new AbortController()
    cancelled = false
    setState({ ...initialState(), phase: 'previewing', canCancel: true })
    try {
      const preview = await options.client.previewExport(
        { version: exportVersion, query: nextQuery },
        request.signal,
      )
      if (cancelled) return false
      query = preview.canonical_query
      if (preview.full_count === 0) {
        setState({ ...initialState(), announcement: 'No data to export.' })
        return false
      }
      setState({
        ...state,
        phase: 'ready',
        preview,
        count: preview.full_count,
        announcement: '',
        canCancel: true,
      })
      return true
    } catch (error) {
      if (cancelled || isAbort(error)) return false
      setState({
        ...state,
        phase: 'failed',
        announcement: 'The export preview could not be loaded. Try again.',
        canCancel: true,
      })
      return false
    } finally {
      request = undefined
    }
  }

  function close(): void {
    request?.abort()
    cancelled = true
    request = undefined
    query = ''
    setState(initialState())
  }

  function setFormat(format: ExportFormat): void {
    if (state.phase === 'exporting') return
    setState({ ...state, format, filename: undefined })
  }

  function setScope(scope: ExportScope): void {
    if (state.phase === 'exporting') return
    setState({
      ...state,
      scope,
      count:
        scope === 'full' ? (state.preview?.full_count ?? 0) : (state.preview?.filtered_count ?? 0),
      filename: undefined,
    })
  }

  async function execute(): Promise<boolean> {
    if (!state.preview || state.phase === 'exporting') return false
    request?.abort()
    request = new AbortController()
    cancelled = false
    const body: ExportBody = {
      version: exportVersion,
      format: state.format,
      scope: state.scope,
      query,
    }
    setState({
      ...state,
      phase: 'exporting',
      announcement: '',
      filename: undefined,
      canCancel: true,
    })
    try {
      const response = await options.client.downloadExport(body, request.signal)
      if (cancelled) return false
      if (!response.ok) {
        setState({
          ...state,
          phase: 'failed',
          announcement: 'The export could not be completed. Try again.',
          canCancel: true,
        })
        return false
      }
      const blob = await response.blob()
      if (cancelled) return false
      const filename = exportFilename(response.headers.get('Content-Disposition'), state.format)
      const actualCount = exportCount(
        response.headers.get('X-Moneyflow-Transaction-Count'),
        state.count,
      )
      publish(root, objectURLs, blob, filename)
      setState({
        ...state,
        phase: 'complete',
        filename,
        announcement: `Exported ${actualCount} ${transactionWord(actualCount)}.`,
        canCancel: false,
      })
      return true
    } catch (error) {
      if (cancelled || isAbort(error)) return false
      setState({
        ...state,
        phase: 'failed',
        announcement: 'The export could not be completed. Try again.',
        canCancel: true,
      })
      return false
    } finally {
      request = undefined
    }
  }

  function cancel(): void {
    if (!state.canCancel) return
    cancelled = true
    request?.abort()
    request = undefined
    if (state.preview) {
      setState({ ...state, phase: 'ready', announcement: 'Export cancelled.', canCancel: true })
    } else {
      setState({ ...initialState(), announcement: 'Export cancelled.' })
    }
  }

  return {
    get state() {
      subscribe()
      return state
    },
    open,
    close,
    setFormat,
    setScope,
    export: execute,
    cancel,
  }
}

function exportCount(value: string | null, fallback: number): number {
  if (value === null || !/^\d+$/.test(value)) return fallback
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : fallback
}

function initialState(): ExportState {
  return {
    phase: 'idle',
    format: 'parquet',
    scope: 'full',
    count: 0,
    announcement: '',
    preview: undefined,
    filename: undefined,
    canCancel: false,
  }
}

function publish(root: Document, objectURLs: ObjectURLs, blob: Blob, filename: string): void {
  const url = objectURLs.createObjectURL(blob)
  const anchor = root.createElement('a')
  try {
    anchor.href = url
    anchor.download = filename
    anchor.hidden = true
    root.body.append(anchor)
    anchor.click()
  } finally {
    anchor.remove()
    objectURLs.revokeObjectURL(url)
  }
}

function exportFilename(disposition: string | null, format: ExportFormat): string {
  const match = disposition?.match(
    /(?:^|;)\s*filename="?([A-Za-z0-9][A-Za-z0-9._-]{0,127})"?(?:;|$)/i,
  )
  return match?.[1] ?? `moneyflow-export.${format}`
}

function transactionWord(count: number): string {
  return count === 1 ? 'transaction' : 'transactions'
}

function isAbort(error: unknown): boolean {
  return (
    (error instanceof DOMException && error.name === 'AbortError') ||
    (typeof error === 'object' && error !== null && 'name' in error && error.name === 'AbortError')
  )
}
