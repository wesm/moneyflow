import { describe, expect, it, vi } from 'vitest'

import type { CatalogClient, ProfileSummary } from '../api/catalog-client'
import { createCatalogController } from './catalog.svelte'

const profiles: ProfileSummary[] = [
  {
    key: 'profile_bbbbbbbbbbbbbbbbbbbbbbbbbb',
    id: 'profile_bbbbbbbbbbbbbbbbbbbbbbbbbb',
    display_name: 'Zulu',
    provider_kind: 'local',
    status: 'local_only',
  },
  {
    key: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa',
    id: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa',
    display_name: 'Alpha',
    provider_kind: 'monarch',
    status: 'ready',
  },
]

describe('catalog controller', () => {
  it('loads profiles in stable alphabetical order without opening one', async () => {
    const client = stubCatalog()
    const controller = createCatalogController({ client })

    await controller.load()

    expect(controller.state.profiles.map((profile) => profile.display_name)).toEqual([
      'Alpha',
      'Zulu',
    ])
    expect(client.list).toHaveBeenCalledTimes(1)
  })

  it('activates a manifestless profile before returning its route identity', async () => {
    const client = stubCatalog()
    const controller = createCatalogController({ client })
    const profile = {
      key: 'legacy',
      display_name: 'Moneyflow',
      provider_kind: 'monarch',
      status: 'ready',
    } satisfies ProfileSummary

    await expect(controller.canonicalID(profile)).resolves.toBe(
      'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa',
    )
    expect(client.activate).toHaveBeenCalledWith('legacy', undefined)
  })

  it('announces sanitized failures and supports recovery preview then apply', async () => {
    const client = stubCatalog()
    const controller = createCatalogController({ client })

    await controller.recovery('profile_aaaaaaaaaaaaaaaaaaaaaaaaaa', false)
    expect(controller.state.recovery?.plan.backup_path).toBe('/synthetic/backup')
    await controller.recovery('profile_aaaaaaaaaaaaaaaaaaaaaaaaaa', true)
    expect(controller.state.announcement).toContain('ready for setup')
  })

  it('rolls back a newly created artifact-free profile', async () => {
    const client = stubCatalog()
    const controller = createCatalogController({ client })

    await expect(controller.cancelNew('profile_aaaaaaaaaaaaaaaaaaaaaaaaaa')).resolves.toBe(true)

    expect(client.cancelNew).toHaveBeenCalledWith('profile_aaaaaaaaaaaaaaaaaaaaaaaaaa')
    expect(controller.state.announcement).toContain('canceled')
  })
})

function stubCatalog(): CatalogClient {
  return {
    mutations: {} as CatalogClient['mutations'],
    list: vi.fn(async () => ({ version: '1', profiles })),
    create: vi.fn(async (displayName, providerKind) => ({
      key: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa',
      id: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa',
      display_name: displayName,
      provider_kind: providerKind,
      status: 'setup_incomplete',
    })),
    activate: vi.fn(async () => profiles[1]!),
    cancelNew: vi.fn(async () => true),
    recovery: vi.fn(async (_profileID, body) => ({
      version: '1',
      plan: {
        profile_id: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa',
        profile_key: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa',
        backup_path: '/synthetic/backup',
        original_code: 'schema_incompatible',
        in_progress: false,
        started_at: '2026-08-18T00:00:00Z',
      },
      recreated: body.confirmed,
      ...(body.confirmed ? { backup_path: '/synthetic/backup' } : {}),
    })),
  }
}
