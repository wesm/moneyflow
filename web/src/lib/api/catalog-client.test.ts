import { beforeEach, describe, expect, it, vi } from 'vitest'

import { createCatalogClient } from './catalog-client'

const profileID = 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa'

describe('profile catalog client', () => {
  beforeEach(() => {
    vi.stubGlobal('location', { origin: 'http://localhost:3000' })
  })

  it('lists profiles without opening one', async () => {
    const upstream = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({
        version: '1',
        profiles: [
          {
            key: profileID,
            id: profileID,
            display_name: 'Example Profile',
            provider_kind: 'monarch',
            status: 'ready',
          },
        ],
      }),
    )
    const client = createCatalogClient('/moneyflow/', upstream)

    await expect(client.list()).resolves.toMatchObject({
      profiles: [{ id: profileID, display_name: 'Example Profile' }],
    })
    const sent = upstream.mock.calls[0]?.[0]
    expect(sent instanceof Request ? sent.url : String(sent)).toContain(
      '/moneyflow/api/v1/profiles',
    )
  })

  it('activates a manifestless local profile with catalog authority', async () => {
    const upstream = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        Response.json({
          mutation_token: 'catalog-token',
          token_expires_at: '2099-01-01T00:00:00Z',
        }),
      )
      .mockResolvedValueOnce(
        Response.json({
          version: '1',
          profile: {
            key: profileID,
            id: profileID,
            display_name: 'Moneyflow',
            provider_kind: 'monarch',
            status: 'ready',
          },
        }),
      )
    const client = createCatalogClient('/moneyflow/', upstream, undefined)

    await expect(client.activate('legacy', 'monarch')).resolves.toMatchObject({ id: profileID })
    expect(String(upstream.mock.calls[0]?.[0])).toContain('/moneyflow/api/v1/bootstrap')
    const request = upstream.mock.calls[1]?.[1]
    expect(request?.headers).toMatchObject({ 'X-Moneyflow-Mutation-Token': 'catalog-token' })
    expect(request?.body).toBe('{"version":"1","key":"legacy","provider_kind":"monarch"}')
  })

  it('cancels a newly created profile with catalog authority', async () => {
    const upstream = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        Response.json({
          mutation_token: 'catalog-token',
          token_expires_at: '2099-01-01T00:00:00Z',
        }),
      )
      .mockResolvedValueOnce(Response.json({ version: '1', removed: true }))
    const client = createCatalogClient('/moneyflow/', upstream, undefined)

    await expect(client.cancelNew(profileID)).resolves.toBe(true)

    expect(String(upstream.mock.calls[1]?.[0])).toContain(`/profiles/${profileID}/cancel`)
  })
})
