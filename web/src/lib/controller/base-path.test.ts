import { describe, expect, it } from 'vitest'

import { apiURL, applicationURL, normalizeBrowserBasePath } from './base-path'

describe('browser base paths', () => {
  it.each([
    ['', '/'],
    ['/', '/'],
    ['moneyflow', '/moneyflow/'],
    ['/moneyflow', '/moneyflow/'],
    ['/nested/moneyflow/', '/nested/moneyflow/'],
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
  })
})
