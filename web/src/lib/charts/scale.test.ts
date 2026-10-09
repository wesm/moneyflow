import { describe, expect, it } from 'vitest'

import { maxMagnitude, ratioBasisPoints } from './scale'

describe('ratioBasisPoints', () => {
  it('floors the magnitude ratio into basis points', () => {
    expect(ratioBasisPoints('1', '3')).toBe(3333)
    expect(ratioBasisPoints('-2', '3')).toBe(6666)
    expect(ratioBasisPoints('3', '-3')).toBe(10000)
    expect(ratioBasisPoints('0', '5')).toBe(0)
  })

  it('stops at 10000 when the value exceeds the maximum', () => {
    expect(ratioBasisPoints('7', '3')).toBe(10000)
    expect(ratioBasisPoints('-9223372036854775808', '1')).toBe(10000)
  })

  it('is 0 when the maximum is 0', () => {
    expect(ratioBasisPoints('0', '0')).toBe(0)
    expect(ratioBasisPoints('7', '0')).toBe(0)
  })

  it('stays exact beyond the safe integer range', () => {
    expect(ratioBasisPoints('9223372036854775806', '9223372036854775807')).toBe(9999)
    expect(ratioBasisPoints('-9223372036854775808', '-9223372036854775808')).toBe(10000)
  })

  it('rejects malformed amounts', () => {
    expect(() => ratioBasisPoints('1.5', '3')).toThrow('is not a minor-unit amount')
    expect(() => ratioBasisPoints('1', '')).toThrow('is not a minor-unit amount')
  })
})

describe('maxMagnitude', () => {
  it('returns the largest absolute value as a minor string', () => {
    expect(maxMagnitude(['120', '-845', '300'])).toBe('845')
    expect(maxMagnitude(['-9223372036854775808', '9223372036854775807'])).toBe(
      '9223372036854775808',
    )
  })

  it('is 0 for no values', () => {
    expect(maxMagnitude([])).toBe('0')
  })

  it('rejects malformed amounts', () => {
    expect(() => maxMagnitude(['1', 'x'])).toThrow('is not a minor-unit amount')
  })
})
