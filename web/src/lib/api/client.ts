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

export interface MoneyflowClient {
  view(body: ViewBody, signal?: AbortSignal): Promise<ViewProjection>
  transition(body: TransitionBody, signal?: AbortSignal): Promise<ViewProjection>
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

  return {
    async view(body, signal) {
      const result = await client.POST('/api/v1/view', requestOptions(body, signal))
      return projectionOrThrow(result.data, result.error)
    },
    async transition(body, signal) {
      const result = await client.POST('/api/v1/view/transition', requestOptions(body, signal))
      return projectionOrThrow(result.data, result.error)
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
