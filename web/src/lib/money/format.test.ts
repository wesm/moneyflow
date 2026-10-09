import { describe, expect, it } from 'vitest'

import cases from './format-cases.json'
import { formatMinor, parseMinor } from './format'

describe('formatMinor', () => {
  it.each(cases)('formats $minor at scale $scale as Go does', ({ minor, scale, display }) => {
    expect(formatMinor(minor, scale)).toBe(display)
  })

  it.each(['', ' 1', '1.5', '+1', '-0', '01', '1e3', '0x10', 'NaN'])(
    'rejects the malformed amount %j',
    (minor) => {
      expect(() => formatMinor(minor, 2)).toThrow('is not a minor-unit amount')
    },
  )

  it.each([-1, 1.5, Number.NaN])('rejects the scale %s', (scale) => {
    expect(() => formatMinor('1', scale)).toThrow('is not a currency scale')
  })
})

describe('parseMinor', () => {
  it('reads int64 extremes exactly', () => {
    expect(parseMinor('-9223372036854775808')).toBe(-9223372036854775808n)
    expect(parseMinor('9223372036854775807')).toBe(9223372036854775807n)
  })

  it('rejects text BigInt would read as zero', () => {
    expect(() => parseMinor('')).toThrow('is not a minor-unit amount')
  })
})
