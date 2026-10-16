import { normalizeBrowserBasePath } from '../lib/controller/base-path'

interface Token {
  value: string
  refreshAt: number
}

const tokenHeader = 'X-Moneyflow-Mutation-Token'
const catalogScope = 'catalog'
// The page's token lives an hour. Every token is renewed five minutes before it expires.
const pageTokenMillis = 55 * 60 * 1000
const earlyRenewalMillis = 5 * 60 * 1000
const profileRoute = /^profiles\/(profile_[a-z2-7]{26})\/(.+)$/
// Profile routes that change the catalog, which the server checks against the catalog token.
const catalogEndpoints = new Set(['recovery', 'cancel', 'remove'])

/**
 * The fetch standalone moneyflow gives its views. Every request omits credentials, and every
 * POST to the API carries the mutation token of its scope: the page's token for the catalog and
 * a bootstrapped token for each profile. An expired token is renewed once and the request sent
 * again. When a bootstrap fails, its response answers the request.
 */
export function createTokenFetch(
  basePath: string,
  upstream: typeof fetch = fetch,
  root: Document = document,
  now: () => number = Date.now,
): typeof fetch {
  const api = new URL(`${normalizeBrowserBasePath(basePath)}api/v1/`, location.origin).href
  const tokens = new Map<string, Token>()
  const pageToken =
    root.querySelector<HTMLMetaElement>('meta[name="moneyflow-mutation-token"]')?.content ?? ''
  if (pageToken !== '') {
    tokens.set(catalogScope, { value: pageToken, refreshAt: now() + pageTokenMillis })
  }

  /**
   * Returns the scope's token, or the response of a bootstrap that failed. A bootstrap stops when
   * `signal`, the signal of the request that waits for it, aborts.
   */
  async function tokenFor(
    scope: string,
    renew: boolean,
    signal: RequestInit['signal'],
  ): Promise<string | Response> {
    const current = tokens.get(scope)
    if (current && !renew && now() < current.refreshAt) return current.value
    const path = scope === catalogScope ? 'bootstrap' : `profiles/${scope}/bootstrap`
    const response = await upstream(`${api}${path}`, {
      method: 'GET',
      cache: 'no-store',
      credentials: 'omit',
      redirect: 'error',
      signal: signal ?? null,
    })
    if (!response.ok) return response
    const value: unknown = await response.json()
    if (!isBootstrap(value)) {
      throw new Error('The Moneyflow mutation bootstrap response is invalid.')
    }
    tokens.set(scope, {
      value: value.mutation_token,
      refreshAt: Date.parse(value.token_expires_at) - earlyRenewalMillis,
    })
    return value.mutation_token
  }

  return async (input, init) => {
    const request = input instanceof Request ? input : undefined
    const url = new URL(request?.url ?? String(input), location.origin).href
    const method = (init?.method ?? request?.method ?? 'GET').toUpperCase()
    if (method !== 'POST' || !url.startsWith(api)) {
      return upstream(input, { ...init, credentials: 'omit' })
    }
    const scope = scopeOf(url.slice(api.length))
    // A Request's body is read into memory, so it can be sent again and stays a plain body.
    const options: RequestInit = request
      ? {
          method: 'POST',
          body: await request.arrayBuffer(),
          redirect: request.redirect,
          signal: request.signal,
        }
      : { ...init }
    const send = (token: string) => {
      const headers = new Headers(init?.headers ?? request?.headers)
      headers.set(tokenHeader, token)
      return upstream(url, { ...options, credentials: 'omit', headers })
    }
    let token = await tokenFor(scope, false, options.signal)
    if (token instanceof Response) return token
    const response = await send(token)
    if (!(await hasProblemCode(response, 'token_expired'))) return response
    token = await tokenFor(scope, true, options.signal)
    if (token instanceof Response) return token
    return send(token)
  }
}

function scopeOf(path: string): string {
  const [, profileID, endpoint] = profileRoute.exec(path) ?? []
  if (!profileID || catalogEndpoints.has(endpoint ?? '')) return catalogScope
  return profileID
}

function isBootstrap(
  value: unknown,
): value is { mutation_token: string; token_expires_at: string } {
  if (typeof value !== 'object' || value === null) return false
  const record = value as Record<string, unknown>
  return (
    typeof record.mutation_token === 'string' &&
    record.mutation_token !== '' &&
    typeof record.token_expires_at === 'string' &&
    Number.isFinite(Date.parse(record.token_expires_at))
  )
}

async function hasProblemCode(response: Response, code: string): Promise<boolean> {
  if (response.ok) return false
  if (!(response.headers.get('Content-Type') ?? '').includes('application/problem+json')) {
    return false
  }
  try {
    const value: unknown = await response.clone().json()
    return typeof value === 'object' && value !== null && 'code' in value && value.code === code
  } catch {
    return false
  }
}
