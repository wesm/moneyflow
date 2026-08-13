import { describe, expect, it, vi } from 'vitest'

import { createMoneyflowClient, MoneyflowProblem } from './client'
import type { ViewProjection } from './client'

describe('Moneyflow generated client adapter', () => {
  it('transports opaque selection and exact money text without decoding either', async () => {
    const opaqueSelection = 'mfsel1.eyJvcGFxdWUiOiJub3QtY2xpZW50LXN0YXRlIn0'
    const hugeMinor = '-9223372036854775808'
    const upstream = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(String(input), init)
      const requestBody = (await request.json()) as { selection: string }
      expect(requestBody.selection).toBe(opaqueSelection)
      return Response.json(projection(opaqueSelection, hugeMinor))
    })
    const client = createMoneyflowClient('/moneyflow/', upstream as typeof fetch)

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
    expect(sent instanceof Request ? sent.url : String(sent)).toContain('/moneyflow/api/v1/view')
  })

  it('throws typed safe problems', async () => {
    const client = createMoneyflowClient(
      '/',
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
      vi.fn(async () => Response.json({ private: 'do-not-echo' }, { status: 500 })) as typeof fetch,
    )

    await expect(client.view({ query: 'v=1', window: { offset: 0, limit: 200 } })).rejects.toThrow(
      'The Moneyflow API returned an invalid response.',
    )
  })
})

function projection(selection: string, minor: string): ViewProjection {
  return {
    api_schema_version: '1',
    projection_schema_version: '1',
    canonical_query: 'v=1',
    selection: selection as ViewProjection['selection'],
    breadcrumbs: [],
    breadcrumb_text: 'All transactions',
    filters: { show_hidden: false, show_transfers: false },
    capabilities: [],
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
    chart: {},
  }
}
