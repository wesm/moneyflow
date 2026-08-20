import createClient from 'openapi-fetch'

import type { components, paths } from './schema'

declare const selectionBrand: unique symbol
export type SelectionValue = string & { readonly [selectionBrand]: true }

export type ViewBody = components['schemas']['ViewBody']
export type TransitionBody = components['schemas']['TransitionBody']
export type ViewProjection = Omit<components['schemas']['Projection'], 'selection'> & {
  selection: SelectionValue
}
export type Problem = components['schemas']['Problem']
export type MutationBody = components['schemas']['MutationBody']
export type MutationInput = components['schemas']['MutationInput']
export type MutationResponse = components['schemas']['MutationResponse']
export type RevisionBody = components['schemas']['RevisionBody']
export type CommitBody = components['schemas']['CommitBody']
export type PendingSummary = components['schemas']['PendingSummary']
export type ReviewBody = components['schemas']['ReviewBody']
export type ReviewTargetsBody = components['schemas']['ReviewTargetsBody']
export type ReviewResponse = components['schemas']['ReviewResponse']
export type DuplicateBody = components['schemas']['DuplicateBody']
export type DuplicateResponse = components['schemas']['DuplicateResponse']
export type ExportBody = components['schemas']['ExportBody']
export type ExportPreviewBody = components['schemas']['ExportPreviewBody']
export type ExportPreviewResponse = components['schemas']['ExportPreviewResponse']
export type EditorCatalog = components['schemas']['EditorCatalogResponse']
export type ProviderStatus = components['schemas']['ProviderStatusResponse']
export type ProviderRefreshBody = components['schemas']['ProviderRefreshBody']
export type ProviderConfirmationBody = components['schemas']['ProviderConfirmationBody']
export type ProviderRefreshResponse = components['schemas']['ProviderRefreshResponse']
export type ProviderWriteStatus = components['schemas']['ProviderWriteStatusResponse']
export type ProviderWriteControlBody = components['schemas']['ProviderWriteControlBody']
export type ProviderWriteReconcileBody = components['schemas']['ProviderWriteReconcileBody']
export type ProviderWriteConfirmationBody = components['schemas']['ProviderWriteConfirmationBody']
export type ProviderWriteResponse = components['schemas']['ProviderWriteResponse']
export type TransactionInformationBody = components['schemas']['TransactionInformationBody']
export type TransactionInformationResponse = components['schemas']['TransactionInformationResponse']

interface BootstrapResponse {
  mutation_token: string
  token_expires_at: string
}

const mutationTokenHeader = 'X-Moneyflow-Mutation-Token'
const proactiveRefreshMillis = 5 * 60 * 1000

export interface MoneyflowClient {
  readonly mutations: MutationFetch
  view(body: ViewBody, signal?: AbortSignal): Promise<ViewProjection>
  transition(body: TransitionBody, signal?: AbortSignal): Promise<ViewProjection>
  projectDuplicates(body: DuplicateBody, signal?: AbortSignal): Promise<DuplicateResponse>
  previewExport(body: ExportPreviewBody, signal?: AbortSignal): Promise<ExportPreviewResponse>
  downloadExport(body: ExportBody, signal?: AbortSignal): Promise<Response>
  providerStatus(signal?: AbortSignal): Promise<ProviderStatus>
  providerWriteStatus(signal?: AbortSignal): Promise<ProviderWriteStatus>
  transactionInformation?(
    body: TransactionInformationBody,
    signal?: AbortSignal,
  ): Promise<TransactionInformationResponse>
}

export interface MutationFetch {
  request(path: string, body: string, signal?: AbortSignal): Promise<Response>
  upload?(path: string, body: FormData, signal?: AbortSignal): Promise<Response>
}

export async function requestProfileJSON<T>(
  transport: MutationFetch,
  path: string,
  body: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const response = await transport.request(path, JSON.stringify(body), signal)
  let value: unknown
  try {
    value = await response.json()
  } catch {
    throw new Error('The Moneyflow profile response is invalid.')
  }
  if (!response.ok) {
    if (isProblem(value)) throw new MoneyflowProblem(value)
    throw new Error('The Moneyflow profile request failed.')
  }
  if (!isRecord(value)) throw new Error('The Moneyflow profile response is invalid.')
  return value as T
}

export function createMutationFetch(
  basePath: string,
  upstream: typeof fetch = fetch,
  root: Document | null | undefined = globalThis.document,
  now: () => number = Date.now,
  bootstrapPath = 'api/v1/bootstrap',
): MutationFetch {
  const origin = globalThis.location?.origin ?? 'http://localhost'
  const baseURL = new URL(basePath, origin)
  let token = readMutationToken(root)
  let refreshAt = token === '' ? 0 : now() + 55 * 60 * 1000

  const refresh = async (signal?: AbortSignal): Promise<void> => {
    const response = await upstream(new URL(bootstrapPath, baseURL), {
      method: 'GET',
      cache: 'no-store',
      credentials: 'omit',
      redirect: 'error',
      ...(signal === undefined ? {} : { signal }),
    })
    const value: unknown = await response.json()
    if (!response.ok || !isBootstrap(value)) {
      throw new Error('The Moneyflow mutation bootstrap response is invalid.')
    }
    token = value.mutation_token
    refreshAt = Date.parse(value.token_expires_at) - proactiveRefreshMillis
  }

  const send = async (
    path: string,
    body: BodyInit,
    signal?: AbortSignal,
    contentType = 'application/json',
  ): Promise<Response> => {
    if (token === '' || now() >= refreshAt) await refresh(signal)
    const options = (): RequestInit => ({
      method: 'POST',
      body,
      credentials: 'omit',
      redirect: 'error',
      headers: {
        ...(contentType === '' ? {} : { 'Content-Type': contentType }),
        [mutationTokenHeader]: token,
      },
      ...(signal === undefined ? {} : { signal }),
    })
    let response = await upstream(new URL(path.replace(/^\/+/, ''), baseURL), options())
    if (await hasProblemCode(response, 'token_expired')) {
      await refresh(signal)
      response = await upstream(new URL(path.replace(/^\/+/, ''), baseURL), options())
    }
    return response
  }

  return {
    request(path, body, signal) {
      return send(path, body, signal)
    },
    upload(path, body, signal) {
      return send(path, body, signal, '')
    },
  }
}

function readMutationToken(root: Document | null | undefined): string {
  return (
    root
      ?.querySelector<HTMLMetaElement>('meta[name="moneyflow-mutation-token"]')
      ?.getAttribute('content') ?? ''
  )
}

function isBootstrap(value: unknown): value is BootstrapResponse {
  if (!isRecord(value)) return false
  return (
    typeof value.mutation_token === 'string' &&
    value.mutation_token !== '' &&
    typeof value.token_expires_at === 'string' &&
    Number.isFinite(Date.parse(value.token_expires_at))
  )
}

async function hasProblemCode(response: Response, code: string): Promise<boolean> {
  if (response.ok) return false
  const contentType = response.headers.get('Content-Type') ?? ''
  if (!contentType.includes('application/problem+json')) return false
  try {
    const value: unknown = await response.clone().json()
    return isRecord(value) && value.code === code
  } catch {
    return false
  }
}

export class MoneyflowProblem extends Error {
  readonly problem: Problem

  constructor(problem: Problem) {
    super(problem.detail)
    this.name = 'MoneyflowProblem'
    this.problem = problem
  }
}

export function createMoneyflowClient(
  basePath: string,
  profileID: string,
  upstream: typeof fetch = fetch,
): MoneyflowClient {
  const origin = globalThis.location?.origin ?? 'http://localhost'
  const client = createClient<paths>({
    baseUrl: new URL(basePath, origin).toString(),
    fetch: upstream,
    redirect: 'error',
  })
  const requestOptions = <Body>(body: Body, signal?: AbortSignal) =>
    signal === undefined ? { body } : { body, signal }
  const profilePrefix = `api/v1/profiles/${profileID}/`
  const scopedMutations = createMutationFetch(
    basePath,
    upstream,
    null,
    Date.now,
    `${profilePrefix}bootstrap`,
  )
  const mutations: MutationFetch = {
    request(path, body, signal) {
      const relative = path.replace(/^\/+/, '')
      if (!relative.startsWith('api/v1/')) {
        throw new Error('The Moneyflow profile API path is invalid.')
      }
      return scopedMutations.request(
        `${profilePrefix}${relative.slice('api/v1/'.length)}`,
        body,
        signal,
      )
    },
    upload(path, body, signal) {
      const relative = path.replace(/^\/+/, '')
      if (!relative.startsWith('api/v1/')) {
        throw new Error('The Moneyflow profile API path is invalid.')
      }
      if (!scopedMutations.upload) throw new Error('Profile uploads are unavailable.')
      return scopedMutations.upload(
        `${profilePrefix}${relative.slice('api/v1/'.length)}`,
        body,
        signal,
      )
    },
  }

  return {
    mutations,
    async view(body, signal) {
      const result = await client.POST('/api/v1/profiles/{profile_id}/view', {
        ...requestOptions(body, signal),
        params: { path: { profile_id: profileID } },
      })
      return projectionOrThrow(result.data, result.error)
    },
    async transition(body, signal) {
      const result = await client.POST('/api/v1/profiles/{profile_id}/view/transition', {
        ...requestOptions(body, signal),
        params: { path: { profile_id: profileID } },
      })
      return projectionOrThrow(result.data, result.error)
    },
    async projectDuplicates(body, signal) {
      const result = await client.POST('/api/v1/profiles/{profile_id}/duplicates', {
        ...requestOptions(body, signal),
        params: { path: { profile_id: profileID } },
      })
      if (isDuplicateResponse(result.data)) return result.data
      if (isProblem(result.error)) throw new MoneyflowProblem(result.error)
      throw new Error('The Moneyflow duplicate response is invalid.')
    },
    async previewExport(body, signal) {
      const result = await client.POST('/api/v1/profiles/{profile_id}/export/preview', {
        ...requestOptions(body, signal),
        params: { path: { profile_id: profileID } },
      })
      if (isExportPreviewResponse(result.data)) return result.data
      if (isProblem(result.error)) throw new MoneyflowProblem(result.error)
      throw new Error('The Moneyflow export preview response is invalid.')
    },
    async downloadExport(body, signal) {
      return await mutations.request('api/v1/export', JSON.stringify(body), signal)
    },
    async providerStatus(signal) {
      const result = await client.GET(
        '/api/v1/profiles/{profile_id}/provider/status',
        signal === undefined
          ? { params: { path: { profile_id: profileID } } }
          : { params: { path: { profile_id: profileID } }, signal },
      )
      if (isProviderStatus(result.data)) return result.data
      if (isProblem(result.error)) throw new MoneyflowProblem(result.error)
      throw new Error('The Moneyflow provider status response is invalid.')
    },
    async providerWriteStatus(signal) {
      const result = await client.GET(
        '/api/v1/profiles/{profile_id}/provider/write-status',
        signal === undefined
          ? { params: { path: { profile_id: profileID } } }
          : { params: { path: { profile_id: profileID } }, signal },
      )
      if (isProviderWriteStatus(result.data)) return result.data
      if (isProblem(result.error)) throw new MoneyflowProblem(result.error)
      throw new Error('The Moneyflow provider write status response is invalid.')
    },
    async transactionInformation(body, signal) {
      const result = await client.POST('/api/v1/profiles/{profile_id}/transaction-information', {
        ...requestOptions(body, signal),
        params: { path: { profile_id: profileID } },
      })
      if (isTransactionInformation(result.data)) return result.data
      if (isProblem(result.error)) throw new MoneyflowProblem(result.error)
      throw new Error('The Moneyflow transaction information response is invalid.')
    },
  }
}

function projectionOrThrow(data: unknown, error: unknown): ViewProjection {
  if (isProjection(data)) return data as ViewProjection
  if (isProblem(error)) throw new MoneyflowProblem(error)
  throw new Error('The Moneyflow API returned an invalid response.')
}

function isProjection(value: unknown): value is components['schemas']['Projection'] {
  if (!isRecord(value)) return false
  return (
    typeof value.api_schema_version === 'string' &&
    typeof value.projection_schema_version === 'string' &&
    typeof value.canonical_query === 'string' &&
    typeof value.selection === 'string'
  )
}

function isDuplicateResponse(value: unknown): value is DuplicateResponse {
  if (!isRecord(value)) return false
  return (
    value.version === '1' &&
    typeof value.revision === 'string' &&
    typeof value.canonical_query === 'string' &&
    typeof value.selection === 'string' &&
    typeof value.total_groups === 'number' &&
    typeof value.total_transactions === 'number' &&
    Array.isArray(value.groups)
  )
}

function isExportPreviewResponse(value: unknown): value is ExportPreviewResponse {
  if (!isRecord(value)) return false
  return (
    value.version === '2' &&
    typeof value.revision === 'string' &&
    Number.isSafeInteger(value.full_count) &&
    Number.isSafeInteger(value.filtered_count) &&
    Number.isSafeInteger(value.active_operations) &&
    Number.isSafeInteger(value.inactive_operations) &&
    typeof value.commit_available === 'boolean' &&
    typeof value.temporary_profile === 'boolean' &&
    typeof value.canonical_query === 'string'
  )
}

function isProblem(value: unknown): value is Problem {
  if (!isRecord(value)) return false
  return (
    typeof value.type === 'string' &&
    typeof value.title === 'string' &&
    typeof value.status === 'number' &&
    typeof value.detail === 'string' &&
    typeof value.code === 'string'
  )
}

function isProviderStatus(value: unknown): value is ProviderStatus {
  if (!isRecord(value)) return false
  return (
    value.version === '1' &&
    typeof value.revision === 'string' &&
    typeof value.generation === 'string' &&
    isRecord(value.progress) &&
    isRecord(value.summary) &&
    isRecord(value.capability)
  )
}

function isProviderWriteStatus(value: unknown): value is ProviderWriteStatus {
  if (!isRecord(value)) return false
  return (
    value.version === '1' &&
    typeof value.revision === 'string' &&
    typeof value.generation === 'string' &&
    typeof value.total === 'number' &&
    typeof value.completed === 'number' &&
    typeof value.remaining === 'number' &&
    (Array.isArray(value.actions) || value.actions === null)
  )
}

function isTransactionInformation(value: unknown): value is TransactionInformationResponse {
  return (
    isRecord(value) &&
    value.version === '1' &&
    typeof value.revision === 'string' &&
    isRecord(value.transaction) &&
    Array.isArray(value.matches)
  )
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
