import { flushSync } from 'svelte'
import { describe, expect, it, vi } from 'vitest'

import type { InsightsBody, InsightsResponse, ViewProjection } from '../api/client'
import { testInsights, testSeries } from '../../test/insights'
import { testProjection } from '../../test/projection'
import { InsightsCache } from './cache.svelte'

interface Call {
  body: InsightsBody
  signal: AbortSignal
  resolve(response: InsightsResponse): void
  reject(error: unknown): void
}

function harness(onNewer = vi.fn<(revision: string) => unknown>()) {
  const calls: Call[] = []
  const cache = new InsightsCache(
    (body, signal) =>
      new Promise<InsightsResponse>((resolve, reject) => {
        calls.push({ body, signal, resolve, reject })
      }),
    onNewer,
  )
  return { cache, calls, onNewer }
}

function view(query: string, revision = '1'): ViewProjection {
  const base = testProjection()
  return testProjection({
    canonical_query: query,
    revision,
    view: { ...base.view, currency: { currency: 'USD', scale: 2 } },
  })
}

/** Answers a request as the server would at `revision`, with a series per requested row. */
async function answer(call: Call | undefined, revision = '1', today = '2026-10-07') {
  if (!call) throw new Error('no insights request was sent')
  const response = testInsights({
    query: call.body.query,
    revision,
    today,
    series: (call.body.rows ?? []).map((identity) => testSeries(identity)),
  })
  if (!call.body.summary) delete response.summary
  call.resolve(response)
  await new Promise((resolve) => setTimeout(resolve, 0))
}

describe('InsightsCache', () => {
  it('keys entries by query and revision and keeps the eight most recently used', async () => {
    const { cache, calls } = harness()
    for (let index = 0; index < 8; index += 1) {
      cache.sync(view(`q=${index}`), [])
      await answer(calls.at(-1))
    }
    cache.sync(view('q=0'), [])
    cache.sync(view('q=8'), [])
    await answer(calls.at(-1))
    expect(calls).toHaveLength(9)

    cache.sync(view('q=0'), [])
    expect(calls).toHaveLength(9)
    expect(cache.status).toBe('ready')
    expect(cache.entry?.query).toBe('q=0')

    cache.sync(view('q=0', '2'), [])
    expect(calls).toHaveLength(10)

    cache.sync(view('q=1'), [])
    expect(calls).toHaveLength(11)
    expect(cache.status).toBe('loading')
  })

  it('fills series incrementally and requests only the rows a new window lacks', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a', 'b'])
    expect(calls[0]?.body).toEqual({ query: 'v=1', rows: ['a', 'b'], summary: true })
    cache.sync(view('v=1'), ['a', 'b'])
    expect(calls).toHaveLength(1)
    await answer(calls[0])

    cache.sync(view('v=1'), ['a', 'b', 'c'])
    expect(calls[1]?.body).toEqual({ query: 'v=1', rows: ['c'], summary: false })
    expect(cache.seriesFor('c')).toBeUndefined()
    await answer(calls[1])

    expect(cache.seriesFor('a')?.identity).toBe('a')
    expect(cache.seriesFor('c')?.identity).toBe('c')
    expect(cache.entry?.response.summary).toBeDefined()
  })

  it('keeps the entry and sends nothing when only the sort changes', async () => {
    const { cache, calls } = harness()
    cache.sync(view('cur=USD%3A2&group=category&v=1'), ['a'])
    await answer(calls[0])
    const shown = cache.entry

    cache.sync(view('cur=USD%3A2&group=category&sort=name%3Aasc&v=1'), ['a'])
    expect(calls).toHaveLength(1)
    expect(cache.entry).toBe(shown)
    expect(cache.stale).toBe(false)
    expect(cache.status).toBe('ready')
    expect(cache.seriesFor('a')?.identity).toBe('a')
  })

  it('reports newer insights once and drops older ones', async () => {
    const { cache, calls, onNewer } = harness()
    cache.sync(view('v=1', '3'), ['a'])
    cache.sync(view('v=1', '3'), ['a', 'b'])
    await answer(calls[0], '4')
    await answer(calls[1], '4')

    expect(onNewer).toHaveBeenCalledTimes(1)
    expect(onNewer).toHaveBeenCalledWith('4')
    expect(cache.entry).toBeUndefined()
    cache.sync(view('v=1', '4'), ['a', 'b'])
    expect(calls).toHaveLength(2)
    expect(cache.entry?.revision).toBe('4')

    cache.sync(view('v=2', '7'), [])
    await answer(calls[2], '6')
    expect(cache.entry?.query).toBe('v=1')
    expect(cache.stale).toBe(true)
    expect(cache.status).toBe('failed')
    expect(onNewer).toHaveBeenCalledTimes(1)

    cache.sync(view('v=2', '6'), [])
    expect(calls).toHaveLength(4)
  })

  it('loads while the view rechecks for newer insights and fails if that brings nothing', async () => {
    let settle!: () => void
    const { cache, calls } = harness(
      vi.fn(() => new Promise<void>((_, reject) => (settle = () => reject(new Error('offline'))))),
    )
    cache.sync(view('v=1', '3'), ['a'])
    await answer(calls[0], '4')
    expect(cache.status).toBe('loading')

    settle()
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(cache.status).toBe('failed')
    cache.retry()
    expect(calls[1]?.body).toEqual({ query: 'v=1', rows: ['a'], summary: true })
  })

  it('reloads the displayed view when a series-only response brings a new day', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a'])
    await answer(calls[0], '1', '2026-10-07')
    cache.sync(view('v=1'), ['a', 'b'])
    await answer(calls[1], '1', '2026-10-08')

    expect(calls[2]?.body).toEqual({ query: 'v=1', rows: ['a'], summary: true })
    expect(cache.status).toBe('loading')
    await answer(calls[2], '1', '2026-10-08')
    expect(cache.status).toBe('ready')
    expect(cache.entry?.response.today).toBe('2026-10-08')
    expect(cache.seriesFor('a')).toBeDefined()
    expect(cache.seriesFor('b')).toBeDefined()
  })

  it('asks again for rows still on their way from an earlier day', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a'])
    await answer(calls[0], '1', '2026-10-07')
    cache.sync(view('v=1'), ['a', 'b'])
    cache.sync(view('v=1'), ['a', 'b', 'c'])
    await answer(calls[1], '1', '2026-10-08')

    expect(calls[2]?.signal.aborted).toBe(true)
    expect(calls[3]?.body).toEqual({ query: 'v=1', rows: ['a', 'c'], summary: true })
  })

  it('asks again for the new day when a late response from an earlier day is dropped', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a'])
    cache.sync(view('v=1'), ['a', 'b'])
    await answer(calls[1], '1', '2026-10-08')
    await answer(calls[0], '1', '2026-10-07')

    expect(cache.entry).toBeUndefined()
    expect(cache.seriesFor('b')).toBeDefined()
    expect(cache.status).toBe('loading')
    expect(calls[2]?.body).toEqual({ query: 'v=1', rows: ['a'], summary: true })
    await answer(calls[2], '1', '2026-10-08')
    expect(cache.status).toBe('ready')
  })

  it('fails a response from an earlier day sent after the new day arrived', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a'])
    await answer(calls[0], '1', '2026-10-08')
    cache.sync(view('v=2'), [])
    await answer(calls[1], '1', '2026-10-07')

    expect(calls).toHaveLength(2)
    expect(cache.status).toBe('failed')
  })

  it('skips the old revision when a new day also brings a newer revision', async () => {
    const { cache, calls, onNewer } = harness()
    cache.sync(view('v=1', '3'), ['a'])
    await answer(calls[0], '3', '2026-10-07')
    cache.sync(view('v=1', '3'), ['a', 'b'])
    await answer(calls[1], '4', '2026-10-08')

    expect(onNewer).toHaveBeenCalledWith('4')
    expect(calls).toHaveLength(2)
  })

  it('clears every entry when today changes', async () => {
    const { cache, calls } = harness()
    cache.sync(view('q=1'), [])
    await answer(calls[0], '1', '2026-10-07')
    cache.sync(view('q=2'), [])
    await answer(calls[1], '1', '2026-10-08')
    expect(cache.entry?.query).toBe('q=2')

    cache.sync(view('q=1'), [])
    expect(calls).toHaveLength(3)
  })

  it('fails without retrying on its own and retry requests again', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a'])
    calls[0]?.reject(new Error('offline'))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(cache.status).toBe('failed')

    cache.sync(view('v=1'), ['a'])
    expect(calls).toHaveLength(1)

    cache.retry()
    expect(cache.status).toBe('loading')
    expect(calls[1]?.body).toEqual({ query: 'v=1', rows: ['a'], summary: true })
    await answer(calls[1])
    expect(cache.status).toBe('ready')
  })

  it('retries only the missing series after a series request fails', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a'])
    cache.sync(view('v=1'), ['a', 'b'])
    await answer(calls[0])
    calls[1]?.reject(new Error('offline'))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(cache.status).toBe('ready')
    expect(cache.seriesFailed).toBe(true)

    cache.sync(view('v=1'), ['a', 'b', 'c'])
    expect(calls).toHaveLength(2)
    cache.retry()
    expect(calls[2]?.body).toEqual({ query: 'v=1', rows: ['b', 'c'], summary: false })
    await answer(calls[2])
    expect(cache.seriesFailed).toBe(false)
    expect(cache.seriesFor('c')).toBeDefined()
  })

  it('writes nothing from an aborted request that still answers', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a'])
    cache.sync(view('v=2'), ['a'])
    await answer(calls[0])

    expect(cache.entry).toBeUndefined()
    cache.sync(view('v=1'), ['a'])
    expect(calls).toHaveLength(3)
  })

  it('aborts requests on dispose and ignores later calls and answers', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a'])
    cache.dispose()

    expect(calls[0]?.signal.aborted).toBe(true)
    await answer(calls[0])
    cache.sync(view('v=2'), ['a'])
    cache.retry()
    expect(calls).toHaveLength(1)
    expect(cache.entry).toBeUndefined()
    expect(cache.status).toBe('loading')
  })

  it('rechecks nothing after dispose, even with a recheck already queued', async () => {
    const onNewer = vi.fn()
    let answered!: Promise<InsightsResponse>
    let resolve!: (response: InsightsResponse) => void
    const cache = new InsightsCache(
      () => (answered = new Promise((done) => (resolve = done))),
      onNewer,
    )
    cache.sync(view('v=1', '3'), ['a'])
    // Runs after the cache reads the answer and before its queued recheck.
    void answered.then(() => cache.dispose())
    resolve(testInsights({ query: 'v=1', revision: '4', series: [testSeries('a')] }))
    await new Promise((done) => setTimeout(done, 0))

    expect(onNewer).not.toHaveBeenCalled()
  })

  it('stays unchanged when a view recheck settles after dispose', async () => {
    let settle!: () => void
    const { cache, calls } = harness(vi.fn(() => new Promise<void>((done) => (settle = done))))
    cache.sync(view('v=1', '3'), ['a'])
    await answer(calls[0], '4')
    cache.dispose()

    settle()
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(cache.status).toBe('loading')
  })

  it('aborts a request for an older key when the key changes', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a'])
    cache.sync(view('v=1', '2'), ['a'])

    expect(calls[0]?.signal.aborted).toBe(true)
    expect(calls[1]?.signal.aborted).toBe(false)
    calls[0]?.reject(new DOMException('aborted', 'AbortError'))
    await answer(calls[1], '2')
    expect(cache.status).toBe('ready')
  })

  it('shows the previous entry as stale until the displayed key arrives', async () => {
    const { cache, calls } = harness()
    cache.sync(view('v=1'), ['a'])
    await answer(calls[0])
    expect(cache.stale).toBe(false)

    cache.sync(view('v=1', '2'), ['a'])
    expect(cache.entry?.revision).toBe('1')
    expect(cache.stale).toBe(true)
    expect(cache.seriesFor('a')?.identity).toBe('a')

    cache.sync(view('v=2', '2'), ['a'])
    expect(cache.entry?.query).toBe('v=1')
    expect(cache.seriesFor('a')).toBeUndefined()

    await answer(calls[2], '2')
    expect(cache.entry?.query).toBe('v=2')
    expect(cache.stale).toBe(false)
  })

  it('redraws its readers without rerunning the effect that syncs it', async () => {
    const { cache, calls } = harness()
    let displayed = $state(view('v=1'))
    let syncs = 0
    const seen: string[] = []
    const stop = $effect.root(() => {
      $effect(() => {
        syncs += 1
        cache.sync(displayed, ['a'])
      })
      $effect(() => {
        seen.push(`${cache.status} ${cache.seriesFor('a')?.identity ?? '-'}`)
      })
    })
    flushSync()
    await answer(calls[0])
    flushSync()
    displayed = view('v=1', '2')
    flushSync()
    stop()

    expect(syncs).toBe(2)
    expect(seen).toEqual(['loading -', 'ready a', 'loading a'])
  })

  it('stays idle and sends nothing without a currency scope', () => {
    const { cache, calls } = harness()
    cache.sync(testProjection(), ['a'])

    expect(calls).toHaveLength(0)
    expect(cache.status).toBe('idle')
    expect(cache.entry).toBeUndefined()
  })
})
