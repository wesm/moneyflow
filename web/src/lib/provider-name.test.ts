import { describe, expect, it } from 'vitest'

import { providerName } from './provider-name'

describe('providerName', () => {
  it('names YNAB profiles and defaults every other kind to Monarch', () => {
    expect(providerName('ynab')).toBe('YNAB')
    expect(providerName('monarch')).toBe('Monarch')
    expect(providerName('local')).toBe('Monarch')
    expect(providerName(undefined)).toBe('Monarch')
  })
})
