import { afterEach, describe, expect, it, vi } from 'vitest'

import { createTokenFetch } from './token-fetch'

const profileID = 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa'
const api = 'http://localhost:3000/moneyflow/api/v1/'

interface Sent {
  url: string
  method: string
  token: string | null
  credentials: RequestCredentials | undefined
  body: string
}

/** Records every request; answers bootstraps with `token` and everything else with `answer`. */
function fakeServer(answer: (call: number) => Response = () => Response.json({ ok: true })) {
  const sent: Sent[] = []
  const upstream = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const target = input instanceof Request ? input : new URL(String(input), location.origin)
    const request = new Request(target, init)
    sent.push({
      url: request.url,
      method: request.method,
      token: request.headers.get('X-Moneyflow-Mutation-Token'),
      credentials: init?.credentials,
      body: await request.text(),
    })
    if (request.url.endsWith('/bootstrap')) {
      return Response.json({
        mutation_token: 'profile-token',
        token_expires_at: '2026-08-14T13:00:00Z',
      })
    }
    return answer(sent.filter((call) => !call.url.endsWith('/bootstrap')).length)
  })
  return { sent, upstream: upstream as typeof fetch }
}

function problem(status: number, code: string): Response {
  return Response.json(
    { type: 'about:blank', title: 'Problem', status, detail: `The request failed: ${code}.`, code },
    { status, headers: { 'Content-Type': 'application/problem+json' } },
  )
}

function post(body = '{}'): RequestInit {
  return { method: 'POST', body, headers: { 'Content-Type': 'application/json' } }
}

const noon = Date.parse('2026-08-14T12:00:00Z')

describe('standalone token fetch', () => {
  afterEach(() => {
    document.head.innerHTML = ''
  })

  it('sends catalog requests with the page token and omits credentials', async () => {
    document.head.innerHTML = '<meta name="moneyflow-mutation-token" content="page-token">'
    const server = fakeServer()
    const tokenFetch = createTokenFetch('/moneyflow/', server.upstream, document, () => noon)

    await tokenFetch(`${api}profiles`, post('{"version":"1"}'))

    expect(server.sent).toEqual([
      {
        url: `${api}profiles`,
        method: 'POST',
        token: 'page-token',
        credentials: 'omit',
        body: '{"version":"1"}',
      },
    ])
  })

  it('bootstraps a profile token once and keeps it until it nears expiry', async () => {
    let now = noon
    const server = fakeServer()
    const tokenFetch = createTokenFetch('/moneyflow/', server.upstream, document, () => now)

    await tokenFetch(`${api}profiles/${profileID}/mutations`, post())
    await tokenFetch(`${api}profiles/${profileID}/undo`, post())
    now = Date.parse('2026-08-14T12:56:00Z')
    await tokenFetch(`${api}profiles/${profileID}/redo`, post())

    expect(server.sent.map(({ url, token }) => [url.slice(api.length), token])).toEqual([
      [`profiles/${profileID}/bootstrap`, null],
      [`profiles/${profileID}/mutations`, 'profile-token'],
      [`profiles/${profileID}/undo`, 'profile-token'],
      [`profiles/${profileID}/bootstrap`, null],
      [`profiles/${profileID}/redo`, 'profile-token'],
    ])
    expect(server.sent[0]?.credentials).toBe('omit')
  })

  it('renews the page token through the catalog bootstrap once it nears expiry', async () => {
    document.head.innerHTML = '<meta name="moneyflow-mutation-token" content="page-token">'
    let now = noon
    const server = fakeServer()
    const tokenFetch = createTokenFetch('/moneyflow/', server.upstream, document, () => now)
    const renewalTime = noon + 55 * 60 * 1000

    now = renewalTime - 1
    await tokenFetch(`${api}profiles`, post())
    now = renewalTime
    await tokenFetch(`${api}profiles`, post())

    expect(server.sent.map(({ url, token }) => [url.slice(api.length), token])).toEqual([
      ['profiles', 'page-token'],
      ['bootstrap', null],
      ['profiles', 'profile-token'],
    ])
  })

  it('sends recovery, cancel and remove with the catalog token', async () => {
    document.head.innerHTML = '<meta name="moneyflow-mutation-token" content="page-token">'
    const server = fakeServer()
    const tokenFetch = createTokenFetch('/moneyflow/', server.upstream, document, () => noon)

    for (const endpoint of ['recovery', 'cancel', 'remove']) {
      await tokenFetch(`${api}profiles/${profileID}/${endpoint}`, post())
    }

    expect(server.sent.map(({ token }) => token)).toEqual([
      'page-token',
      'page-token',
      'page-token',
    ])
  })

  it('renews an expired token once and sends the same request again', async () => {
    document.head.innerHTML = '<meta name="moneyflow-mutation-token" content="page-token">'
    const server = fakeServer((call) =>
      call === 1 ? problem(403, 'token_expired') : Response.json({ ok: true }),
    )
    const tokenFetch = createTokenFetch('/moneyflow/', server.upstream, document, () => noon)

    const response = await tokenFetch(`${api}profiles/activate`, post('{"key":"legacy"}'))

    expect(response.ok).toBe(true)
    expect(server.sent.map(({ url, token, body }) => [url.slice(api.length), token, body])).toEqual(
      [
        ['profiles/activate', 'page-token', '{"key":"legacy"}'],
        ['bootstrap', null, ''],
        ['profiles/activate', 'profile-token', '{"key":"legacy"}'],
      ],
    )
  })

  it('returns other problems without a retry', async () => {
    document.head.innerHTML = '<meta name="moneyflow-mutation-token" content="page-token">'
    const server = fakeServer(() => problem(409, 'revision_conflict'))
    const tokenFetch = createTokenFetch('/moneyflow/', server.upstream, document, () => noon)

    const response = await tokenFetch(`${api}profiles`, post())

    expect(response.status).toBe(409)
    expect(server.sent).toHaveLength(1)
  })

  it('tokens a request that arrives as a Request object', async () => {
    const server = fakeServer()
    const tokenFetch = createTokenFetch('/moneyflow/', server.upstream, document, () => noon)

    await tokenFetch(new Request(`${api}profiles/${profileID}/view`, post('{"query":"v=1"}')))

    expect(server.sent.at(-1)).toEqual({
      url: `${api}profiles/${profileID}/view`,
      method: 'POST',
      token: 'profile-token',
      credentials: 'omit',
      body: '{"query":"v=1"}',
    })
  })

  it('keeps the headers of a Request input and adds the token', async () => {
    document.head.innerHTML = '<meta name="moneyflow-mutation-token" content="page-token">'
    const upstream = vi.fn<typeof fetch>(async () => Response.json({ ok: true }))
    const tokenFetch = createTokenFetch('/moneyflow/', upstream, document, () => noon)

    await tokenFetch(
      new Request(`${api}profiles`, {
        method: 'POST',
        body: '{}',
        headers: { 'Content-Type': 'application/json', 'X-Example': 'kept' },
      }),
    )

    const headers = new Headers(upstream.mock.calls[0]?.[1]?.headers)
    expect(Object.fromEntries(headers)).toEqual({
      'content-type': 'application/json',
      'x-example': 'kept',
      'x-moneyflow-mutation-token': 'page-token',
    })
  })

  it('aborts the outgoing request when the signal of a Request input aborts', async () => {
    document.head.innerHTML = '<meta name="moneyflow-mutation-token" content="page-token">'
    const upstream = vi.fn<typeof fetch>(async (_input, init) => {
      init?.signal?.throwIfAborted()
      return Response.json({ ok: true })
    })
    const tokenFetch = createTokenFetch('/moneyflow/', upstream, document, () => noon)
    const controller = new AbortController()
    const request = new Request(`${api}profiles`, { ...post(), signal: controller.signal })
    controller.abort()

    await expect(tokenFetch(request)).rejects.toMatchObject({ name: 'AbortError' })
  })

  it.each([
    ['its first token', 1],
    ['a renewed token', 2],
  ])('stops waiting for %s once the request is canceled', async (_name, stalled) => {
    const waiting = Promise.withResolvers<AbortSignal | null | undefined>()
    let bootstraps = 0
    const upstream = vi.fn<typeof fetch>(async (input, init) => {
      if (!String(input).endsWith('/bootstrap')) return problem(403, 'token_expired')
      bootstraps += 1
      if (bootstraps < stalled) {
        return Response.json({
          mutation_token: 'profile-token',
          token_expires_at: '2026-08-14T13:00:00Z',
        })
      }
      // A bootstrap that does not answer; as fetch does, it gives up once its signal aborts.
      waiting.resolve(init?.signal)
      return await new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(init.signal?.reason))
      })
    })
    const tokenFetch = createTokenFetch('/moneyflow/', upstream, document, () => noon)
    const controller = new AbortController()
    const sent = tokenFetch(`${api}profiles/${profileID}/provider/refresh`, {
      ...post(),
      signal: controller.signal,
    })
    const signal = await waiting.promise
    controller.abort()

    expect(signal?.aborted).toBe(true)
    await expect(sent).rejects.toMatchObject({ name: 'AbortError' })
  })

  it('keeps the redirect mode of a Request input', async () => {
    document.head.innerHTML = '<meta name="moneyflow-mutation-token" content="page-token">'
    const upstream = vi.fn<typeof fetch>(async () => Response.json({ ok: true }))
    const tokenFetch = createTokenFetch('/moneyflow/', upstream, document, () => noon)

    await tokenFetch(new Request(`${api}profiles`, { ...post(), redirect: 'error' }))

    expect(upstream.mock.calls[0]?.[1]?.redirect).toBe('error')
  })

  it('answers a request with the failed bootstrap and does not send it', async () => {
    const upstream = vi.fn<typeof fetch>(async () => problem(404, 'not_found'))
    const tokenFetch = createTokenFetch('/moneyflow/', upstream, document, () => noon)

    const response = await tokenFetch(`${api}profiles/${profileID}/view`, post())

    expect(response.status).toBe(404)
    expect(upstream).toHaveBeenCalledTimes(1)
  })

  it('passes reads and requests outside the API through with credentials omitted', async () => {
    document.head.innerHTML = '<meta name="moneyflow-mutation-token" content="page-token">'
    const server = fakeServer()
    const tokenFetch = createTokenFetch('/moneyflow/', server.upstream, document, () => noon)

    await tokenFetch(`${api}profiles/${profileID}/provider/status`)
    await tokenFetch('/moneyflow/openapi.json', post())

    expect(
      server.sent.map(({ method, token, credentials }) => [method, token, credentials]),
    ).toEqual([
      ['GET', null, 'omit'],
      ['POST', null, 'omit'],
    ])
  })
})
