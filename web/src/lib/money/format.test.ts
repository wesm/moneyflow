import { describe, expect, it } from 'vitest'

import cases from './format-cases.json'
import { dashZero, formatMinor, parseMinor, tenthsPercent, unsignedZero } from './format'

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

describe('dashZero', () => {
  it('shows a zero In or Out as a dash and keeps every other amount', () => {
    expect(dashZero('+0.00')).toBe('—')
    expect(dashZero('+0')).toBe('—')
    expect(dashZero('+0.01')).toBe('+0.01')
    expect(dashZero('-480.00')).toBe('-480.00')
    expect(dashZero('+10.00')).toBe('+10.00')
  })
})

describe('unsignedZero', () => {
  it('shows a zero Net without its sign and keeps every other amount', () => {
    expect(unsignedZero('+0.00')).toBe('0.00')
    expect(unsignedZero('+0')).toBe('0')
    expect(unsignedZero('+0.01')).toBe('+0.01')
    expect(unsignedZero('-5.00')).toBe('-5.00')
  })
})

describe('tenthsPercent', () => {
  it('formats tenths of a percent with one decimal and a sign only when negative', () => {
    expect(tenthsPercent(62)).toBe('6.2%')
    expect(tenthsPercent(1000)).toBe('100.0%')
    expect(tenthsPercent(0)).toBe('0.0%')
    expect(tenthsPercent(-37)).toBe('-3.7%')
  })
})
