import { describe, expect, it } from 'vitest'

import { assetBase, parseApplicationRoute, profileAPIBase, profileApplicationPath } from './routing'

const profileID = 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa'

describe('application routing', () => {
  it('parses the catalog route', () => {
    expect(
      parseApplicationRoute('/moneyflow/', new URL('https://example.test/moneyflow/?v=1')),
    ).toEqual({ kind: 'catalog' })
  })

  it('parses a profile route without consuming analytical query state', () => {
    const location = new URL(`https://example.test/moneyflow/p/${profileID}/?v=1&group=merchant`)
    expect(parseApplicationRoute('/moneyflow/', location)).toEqual({
      kind: 'profile',
      profileID,
    })
    expect(location.search).toBe('?v=1&group=merchant')
  })

  it.each([
    '/moneyflow/p/legacy/',
    '/moneyflow/p/profile_AAAAAAAAAAAAAAAAAAAAAAAAAA/',
    `/moneyflow/p/${profileID}`,
    `/moneyflow/p/${profileID}/extra`,
    '/moneyflow/accounts',
    '/outside/',
  ])('rejects a noncanonical application route %s', (pathname) => {
    expect(() =>
      parseApplicationRoute('/moneyflow/', new URL(`https://example.test${pathname}`)),
    ).toThrow('route is invalid')
  })

  it('keeps profile APIs and application paths separate from the asset base', () => {
    expect(profileAPIBase('/moneyflow/', profileID)).toBe(
      `/moneyflow/api/v1/profiles/${profileID}/`,
    )
    expect(profileApplicationPath('/moneyflow/', profileID)).toBe(`/moneyflow/p/${profileID}/`)
    expect(assetBase('/moneyflow/')).toBe('/moneyflow/')
  })
})
