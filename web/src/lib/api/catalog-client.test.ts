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

  it('activates a manifestless local profile with one JSON request', async () => {
    const upstream = vi.fn<typeof fetch>().mockResolvedValueOnce(
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
    const client = createCatalogClient('/moneyflow/', upstream)

    await expect(client.activate('legacy', 'monarch')).resolves.toMatchObject({ id: profileID })
    expect(upstream).toHaveBeenCalledTimes(1)
    expect(String(upstream.mock.calls[0]?.[0])).toContain('/moneyflow/api/v1/profiles/activate')
    const request = upstream.mock.calls[0]?.[1]
    expect(request?.headers).toEqual({ 'Content-Type': 'application/json' })
    expect(request?.body).toBe('{"version":"1","key":"legacy","provider_kind":"monarch"}')
  })

  it('cancels a newly created profile with one request', async () => {
    const upstream = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(Response.json({ version: '1', removed: true }))
    const client = createCatalogClient('/moneyflow/', upstream)

    await expect(client.cancelNew(profileID)).resolves.toBe(true)

    expect(upstream).toHaveBeenCalledTimes(1)
    expect(String(upstream.mock.calls[0]?.[0])).toContain(`/profiles/${profileID}/cancel`)
  })
})
