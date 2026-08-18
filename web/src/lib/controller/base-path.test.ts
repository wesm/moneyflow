import { describe, expect, it } from 'vitest'

import { apiURL, applicationURL, normalizeBrowserBasePath, readBasePath } from './base-path'

describe('browser base paths', () => {
  it.each([
    ['', '/'],
    ['/', '/'],
    ['moneyflow', '/moneyflow/'],
    ['/moneyflow', '/moneyflow/'],
    ['/nested/moneyflow/', '/nested/moneyflow/'],
    ['/__moneyflow__/', '/__moneyflow__/'],
  ])('normalizes %s', (input, expected) => {
    expect(normalizeBrowserBasePath(input)).toBe(expected)
  })

  it.each([
    '//',
    '/a//b',
    '/a/../b',
    '/a/./b',
    '/a\\b',
    '/a?b',
    '/a#b',
    '/a/%2f/b',
    '/a/%5C/b',
    '/a/%252f/b',
    '/a/%255c/b',
    '/a/%253f/b',
    '/a%zz',
    'https://example.com/a',
  ])('rejects malformed base path %s', (input) =>
    expect(() => normalizeBrowserBasePath(input)).toThrow('invalid'),
  )

  it('builds API and canonical application URLs without double slashes', () => {
    expect(apiURL('/moneyflow/', 'api/v1/view')).toBe('/moneyflow/api/v1/view')
    expect(applicationURL('/moneyflow/', 'v=1&search=coffee%20shop')).toBe(
      '/moneyflow/?v=1&search=coffee%20shop',
    )
    expect(applicationURL('/', '')).toBe('/')
    expect(
      applicationURL('/moneyflow/p/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/', 'v=1&group=merchant'),
    ).toBe('/moneyflow/p/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/?v=1&group=merchant')
  })

  it('reads an injected base path without rejecting legitimate double underscores', () => {
    const documentValue = {
      querySelector: () => ({ getAttribute: () => '/__moneyflow__/' }),
    } as unknown as Document
    expect(readBasePath(documentValue)).toBe('/__moneyflow__/')
  })
})
