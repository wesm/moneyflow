import createClient from 'openapi-fetch'

import type { components, paths } from './schema'
import { createMutationFetch, MoneyflowProblem, type MutationFetch, type Problem } from './client'

export type ProfileCatalogResponse = components['schemas']['ProfileCatalogResponse']
export type ProfileSummary = components['schemas']['ProfileSummary']
export type ProfileCreateBody = components['schemas']['ProfileCreateBody']
export type ProfileResponse = components['schemas']['ProfileResponse']
export type RecoveryBody = components['schemas']['RecoveryBody']
export type RecoveryResponse = components['schemas']['RecoveryResponse']

export interface CatalogClient {
  readonly mutations: MutationFetch
  list(signal?: AbortSignal): Promise<ProfileCatalogResponse>
  create(displayName: string, providerKind: 'monarch' | 'local'): Promise<ProfileSummary>
  activate(key: string): Promise<ProfileSummary>
  recovery(profileID: string, body: RecoveryBody): Promise<RecoveryResponse>
}

export function createCatalogClient(
  basePath: string,
  upstream: typeof fetch = fetch,
  root: Document | null | undefined = globalThis.document,
): CatalogClient {
  const origin = globalThis.location?.origin ?? 'http://localhost'
  const client = createClient<paths>({
    baseUrl: new URL(basePath, origin).toString(),
    fetch: upstream,
    redirect: 'error',
  })
  const mutations = createMutationFetch(basePath, upstream, root)

  return {
    mutations,
    async list(signal) {
      const result = await client.GET('/api/v1/profiles', signal === undefined ? {} : { signal })
      if (validCatalog(result.data)) return result.data
      fail(result.error, 'The Moneyflow profile catalog response is invalid.')
    },
    async create(displayName, providerKind) {
      const response = await requestCatalogJSON<ProfileResponse>(mutations, 'api/v1/profiles', {
        version: '1',
        display_name: displayName,
        provider_kind: providerKind,
      } satisfies ProfileCreateBody)
      if (!validProfileResponse(response)) invalidCatalogResponse()
      return response.profile
    },
    async activate(key) {
      const response = await requestCatalogJSON<ProfileResponse>(
        mutations,
        'api/v1/profiles/activate',
        { version: '1', key },
      )
      if (!validProfileResponse(response)) invalidCatalogResponse()
      return response.profile
    },
    async recovery(profileID, body) {
      const response = await requestCatalogJSON<RecoveryResponse>(
        mutations,
        `api/v1/profiles/${profileID}/recovery`,
        body,
      )
      if (!isRecord(response) || response.version !== '1' || !isRecord(response.plan)) {
        invalidCatalogResponse()
      }
      return response
    },
  }
}

async function requestCatalogJSON<T>(
  transport: MutationFetch,
  path: string,
  body: unknown,
): Promise<T> {
  const response = await transport.request(path, JSON.stringify(body))
  let value: unknown
  try {
    value = await response.json()
  } catch {
    invalidCatalogResponse()
  }
  if (!response.ok) {
    if (validProblem(value)) throw new MoneyflowProblem(value)
    throw new Error('The Moneyflow profile catalog request failed.')
  }
  if (!isRecord(value)) invalidCatalogResponse()
  return value as T
}

function validCatalog(value: unknown): value is ProfileCatalogResponse {
  return isRecord(value) && value.version === '1' && Array.isArray(value.profiles)
}

function validProfileResponse(value: unknown): value is ProfileResponse {
  return isRecord(value) && value.version === '1' && isRecord(value.profile)
}

function validProblem(value: unknown): value is Problem {
  return (
    isRecord(value) &&
    typeof value.type === 'string' &&
    typeof value.title === 'string' &&
    typeof value.status === 'number' &&
    typeof value.detail === 'string' &&
    typeof value.code === 'string'
  )
}

function fail(error: unknown, message: string): never {
  if (validProblem(error)) throw new MoneyflowProblem(error)
  throw new Error(message)
}

function invalidCatalogResponse(): never {
  throw new Error('The Moneyflow profile catalog response is invalid.')
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
