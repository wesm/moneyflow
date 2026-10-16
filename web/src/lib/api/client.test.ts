import { describe, expect, it, vi } from 'vitest'

import { createMoneyflowClient, createMutationFetch, MoneyflowProblem } from './client'
import type { ViewProjection } from './client'
import { testInsights, testSeries } from '../../test/insights'

const profileID = 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa'

describe('Moneyflow generated client adapter', () => {
  it('projects bounded duplicates beneath the profile path without mutation transport', async () => {
    const upstream = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(String(input), init)
      expect(request.headers.get('X-Moneyflow-Mutation-Token')).toBeNull()
      return Response.json({
        version: '1',
        revision: '3',
        canonical_query: 'v=1',
        selection: 'mfsel1.opaque',
        selection_count: 0,
        total_groups: 1,
        total_transactions: 2,
        group_window: { offset: 0, limit: 20, count: 1 },
        row_window: { offset: 0, limit: 20, count: 2 },
        groups: [],
      })
    })
    const client = createMoneyflowClient('/moneyflow/', profileID, upstream as typeof fetch)

    await expect(
      client.projectDuplicates({
        version: '1',
        expected_revision: '3',
        query: 'v=1',
        group_window: { offset: 0, limit: 20 },
        row_window: { offset: 0, limit: 20 },
      }),
    ).resolves.toMatchObject({ total_groups: 1, total_transactions: 2 })
    const sent = upstream.mock.calls[0]?.[0]
    expect(sent instanceof Request ? sent.url : String(sent)).toContain(
      `/moneyflow/api/v1/profiles/${profileID}/duplicates`,
    )
  })

  it('projects insights beneath the profile path without mutation transport', async () => {
    const upstream = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(String(input), init)
      expect(request.headers.get('X-Moneyflow-Mutation-Token')).toBeNull()
      expect(await request.json()).toEqual({ query: 'v=1', rows: ['agg-1'], summary: true })
      return Response.json(
        testInsights({ revision: '5', series: [testSeries('agg-1', '-9223372036854775808')] }),
      )
    })
    const client = createMoneyflowClient('/moneyflow/', profileID, upstream as typeof fetch)

    const response = await client.insights({ query: 'v=1', rows: ['agg-1'], summary: true })

    expect(response.revision).toBe('5')
    expect(response.series[0]?.trend).toEqual(['-9223372036854775808'])
    const sent = upstream.mock.calls[0]?.[0]
    expect(sent instanceof Request ? sent.url : String(sent)).toContain(
      `/moneyflow/api/v1/profiles/${profileID}/view/insights`,
    )
  })

  it('rejects insights problems and malformed insights', async () => {
    const problem = createMoneyflowClient(
      '/',
      profileID,
      vi.fn(async () =>
        Response.json(
          {
            type: 'about:blank',
            title: 'Unprocessable Entity',
            status: 422,
            detail: 'A row does not belong to the view.',
            code: 'invalid_insights_request',
          },
          { status: 422, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ) as typeof fetch,
    )
    await expect(problem.insights({ query: 'v=1' })).rejects.toMatchObject({
      name: 'MoneyflowProblem',
      problem: { code: 'invalid_insights_request' },
    })

    const malformed = createMoneyflowClient(
      '/',
      profileID,
      vi.fn(async () => Response.json({ version: '1', revision: 5 })) as typeof fetch,
    )
    await expect(malformed.insights({ query: 'v=1' })).rejects.toThrow(
      'The Moneyflow insights response is invalid.',
    )
  })

  it.each(['', '-1', '1.0', 'v5'])('rejects the insights revision %j', async (revision) => {
    const client = createMoneyflowClient(
      '/',
      profileID,
      vi.fn(async () => Response.json(testInsights({ revision }))) as typeof fetch,
    )

    await expect(client.insights({ query: 'v=1' })).rejects.toThrow(
      'The Moneyflow insights response is invalid.',
    )
  })

  it('previews committed export counts without mutation transport', async () => {
    const upstream = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(String(input), init)
      expect(request.headers.get('X-Moneyflow-Mutation-Token')).toBeNull()
      expect(await request.json()).toEqual({ version: '2', query: 'group=merchant&v=1' })
      return Response.json({
        version: '2',
        revision: '4',
        full_count: 12,
        filtered_count: 3,
        active_operations: 2,
        inactive_operations: 1,
        commit_available: true,
        temporary_profile: false,
        canonical_query: 'group=merchant&v=1',
      })
    })
    const client = createMoneyflowClient('/moneyflow/', profileID, upstream as typeof fetch)

    await expect(
      client.previewExport({ version: '2', query: 'group=merchant&v=1' }),
    ).resolves.toMatchObject({ full_count: 12, filtered_count: 3 })
    const sent = upstream.mock.calls[0]?.[0]
    expect(sent instanceof Request ? sent.url : String(sent)).toContain(
      `/moneyflow/api/v1/profiles/${profileID}/export/preview`,
    )
  })

  it('returns the export response without decoding its body or headers', async () => {
    const bytes = new Uint8Array([80, 65, 82, 49])
    const upstream = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(String(input), init)
      expect(request.url).toBe(
        `http://localhost:3000/moneyflow/api/v1/profiles/${profileID}/export`,
      )
      expect(request.headers.get('X-Moneyflow-Mutation-Token')).toBeNull()
      expect(await request.json()).toEqual({
        version: '2',
        format: 'parquet',
        scope: 'full',
        query: 'v=1',
      })
      return new Response(bytes, {
        headers: {
          'Content-Disposition': 'attachment; filename="moneyflow-export.parquet"',
          'Content-Type': 'application/vnd.apache.parquet',
        },
      })
    })
    const client = createMoneyflowClient('/moneyflow/', profileID, upstream as typeof fetch)

    const response = await client.downloadExport({
      version: '2',
      format: 'parquet',
      scope: 'full',
      query: 'v=1',
    })

    expect(response.headers.get('Content-Disposition')).toContain('moneyflow-export.parquet')
    expect(new Uint8Array(await response.arrayBuffer())).toEqual(bytes)
    expect(upstream).toHaveBeenCalledTimes(1)
  })

  it('loads credential-blind provider write status beneath the profile path', async () => {
    const upstream = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      void input
      void init
      return Response.json({
        version: '1',
        revision: '3',
        generation: '2',
        batch_version: '4',
        phase: 'writing',
        total: 5,
        completed: 2,
        failed: 0,
        remaining: 3,
        overrides: 0,
        actions: ['pause'],
      })
    })
    const client = createMoneyflowClient('/moneyflow/', profileID, upstream as typeof fetch)

    await expect(client.providerWriteStatus()).resolves.toMatchObject({
      batch_version: '4',
      phase: 'writing',
      remaining: 3,
    })
    const sent = upstream.mock.calls[0]?.[0]
    expect(sent instanceof Request ? sent.url : String(sent)).toContain(
      '/moneyflow/api/v1/profiles/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/provider/write-status',
    )
  })

  it('loads counts-only provider status beneath the configured base path', async () => {
    const upstream = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      void input
      void init
      return Response.json({
        version: '1',
        revision: '3',
        generation: '2',
        progress: { fetched: 20, total: 40 },
        summary: {
          imported_accounts: 0,
          imported_merchants: 0,
          imported_groups: 0,
          imported_categories: 0,
          imported_transactions: 40,
          removed_transactions: 0,
          removed_operations: 0,
          removed_targets: 0,
          retained_operations: 0,
          rebased_hide_targets: 0,
          discarded_redo_operations: 0,
        },
        capability: {
          id: 'provider.refresh',
          key_display: 'r',
          description: 'Refresh provider data',
          category: 'System',
          available: true,
        },
      })
    })
    const client = createMoneyflowClient('/moneyflow/', profileID, upstream as typeof fetch)

    await expect(client.providerStatus()).resolves.toMatchObject({ revision: '3' })
    const sent = upstream.mock.calls[0]?.[0]
    expect(sent instanceof Request ? sent.url : String(sent)).toContain(
      '/moneyflow/api/v1/profiles/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/provider/status',
    )
  })

  it('transports opaque selection and exact money text without decoding either', async () => {
    const opaqueSelection = 'mfsel1.eyJvcGFxdWUiOiJub3QtY2xpZW50LXN0YXRlIn0'
    const hugeMinor = '-9223372036854775808'
    const upstream = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(String(input), init)
      const requestBody = (await request.json()) as { selection: string }
      expect(requestBody.selection).toBe(opaqueSelection)
      return Response.json(projection(opaqueSelection, hugeMinor))
    })
    const client = createMoneyflowClient('/moneyflow/', profileID, upstream as typeof fetch)

    const result = await client.view({
      query: 'v=1',
      selection: opaqueSelection,
      window: { offset: 0, limit: 200 },
    })

    expect(String(result.selection)).toBe(opaqueSelection)
    expect(result.detail_rows?.[0]?.amount.minor).toBe(hugeMinor)
    expect(result.detail_rows?.[0]?.amount.decimal).toBe('-92233720368547758.08')
    expect(upstream).toHaveBeenCalledTimes(1)
    const sent = upstream.mock.calls[0]?.[0]
    expect(sent instanceof Request ? sent.url : String(sent)).toContain(
      '/moneyflow/api/v1/profiles/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/view',
    )
  })

  it('throws typed safe problems', async () => {
    const client = createMoneyflowClient(
      '/',
      profileID,
      vi.fn(async () =>
        Response.json(
          {
            type: 'about:blank',
            title: 'Conflict',
            status: 409,
            detail: 'The selection is too large.',
            code: 'selection_too_large',
          },
          { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ) as typeof fetch,
    )

    const request = client.view({ query: 'v=1', window: { offset: 0, limit: 200 } })
    await expect(request).rejects.toBeInstanceOf(MoneyflowProblem)
    await expect(request).rejects.toMatchObject({
      name: 'MoneyflowProblem',
      problem: { code: 'selection_too_large' },
    })
  })

  it('rejects malformed error payloads without echoing them', async () => {
    const client = createMoneyflowClient(
      '/',
      profileID,
      vi.fn(async () => Response.json({ private: 'do-not-echo' }, { status: 500 })) as typeof fetch,
    )

    await expect(client.view({ query: 'v=1', window: { offset: 0, limit: 200 } })).rejects.toThrow(
      'The Moneyflow API returned an invalid response.',
    )
  })
})

describe('Moneyflow mutation transport', () => {
  it('sends JSON beneath the profile path with no token and no credentials option', async () => {
    const upstream = vi.fn<typeof fetch>(async () => Response.json({ revision: '2' }))
    const client = createMoneyflowClient('/moneyflow/', profileID, upstream)

    await client.mutations.request('api/v1/mutations', '{"action":"transaction.hide"}')

    expect(upstream).toHaveBeenCalledTimes(1)
    const [url, init] = upstream.mock.calls[0] ?? []
    expect(String(url)).toBe(
      `http://localhost:3000/moneyflow/api/v1/profiles/${profileID}/mutations`,
    )
    expect(init).toEqual({
      method: 'POST',
      body: '{"action":"transaction.hide"}',
      redirect: 'error',
      headers: { 'Content-Type': 'application/json' },
    })
  })

  it('uploads form data without setting its content type', async () => {
    const upstream = vi.fn<typeof fetch>(async () => Response.json({}))
    const form = new FormData()
    form.set('version', '1')

    await createMutationFetch('/moneyflow/', upstream).upload?.('api/v1/files', form)

    expect(upstream.mock.calls[0]?.[1]).toEqual({ method: 'POST', body: form, redirect: 'error' })
  })
})

function projection(selection: string, minor: string): ViewProjection {
  return {
    api_schema_version: '1',
    projection_schema_version: '1',
    profile_kind: 'local',
    amazon_match_column: false,
    revision: '0',
    pending: { active_operations: 0, inactive_operations: 0, affected_transactions: 0 },
    canonical_query: 'v=1',
    view: {
      mode: 'detail',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'date',
      sort_direction: 'desc',
      sort_fields: ['date', 'merchant', 'category', 'account', 'amount'],
      drill_depth: 0,
      root: { mode: 'detail', grouping: 'merchant', time_granularity: 'year' },
    },
    selection: selection as ViewProjection['selection'],
    selection_count: 0,
    all_rows_selected: false,
    breadcrumbs: [],
    breadcrumb_text: 'All transactions',
    filters: { show_hidden: false, show_transfers: false },
    capabilities: [],
    currencies: [],
    total_rows: 1,
    window: { offset: 0, limit: 200, count: 1 },
    detail_rows: [
      {
        index: 0,
        identity: 'detail:txn-1',
        date: '2024-01-02',
        account: 'Account Name',
        merchant: 'Example Merchant',
        category: 'Example Category',
        group: 'Example Group',
        amount: {
          minor,
          currency: 'USD',
          scale: 2,
          decimal: '-92233720368547758.08',
          display: '-$92,233,720,368,547,758.08',
        },
        flags: { selected: false, hidden: false, pending: false },
      },
    ],
    statistics: [],
  }
}
